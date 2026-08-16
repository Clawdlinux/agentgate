package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Clawdlinux/agentgate/internal/admin"
	"github.com/Clawdlinux/agentgate/internal/auth"
	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/gateway"
	"github.com/Clawdlinux/agentgate/internal/oauth"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func testMasterKey() []byte {
	return []byte("01234567890123456789012345678901")
}

// setupIntegration wires the real composition: a file-backed SQLite
// database with every migration applied, the real auth.KeyStore, the real
// vault.SQLiteStore, the real signer.Store, and the real receipt.Ledger —
// the same dependency graph cmd/agentgw/main.go builds in production. A
// real file (not ":memory:") is used because Ledger.Append pins a
// dedicated connection per transaction; an unshared ":memory:" database
// would appear empty on that second connection.
func setupIntegration(t *testing.T, upstreamURL string) (*httptest.Server, *sql.DB, string) {
	t.Helper()

	database, err := agentgatedb.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}

	// Set up registry.
	reg := registry.New()
	baseURL := upstreamURL
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	_ = reg.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth:
      type: oauth2
    actions:
      list_repos:
        method: GET
        path: /user/repos
      create_issue:
        method: POST
        path: /repos/{owner}/{repo}/issues
        params:
          owner: string
          repo: string
          title: string
  stripe:
    base_url: %s
    auth:
      type: bearer
    actions:
      list_invoices:
        method: GET
        path: /invoices
`, baseURL, baseURL)))

	// Vault, signer, and receipt ledger all derive from one master key, as
	// cmd/agentgw/main.go does.
	masterKey := testMasterKey()
	vaultStore, err := vault.NewSQLiteStore(database, masterKey)
	if err != nil {
		t.Fatal(err)
	}

	keyStore := auth.NewKeyStore(database)
	_, apiKey, _ := keyStore.Create(t.Context(), "test-agent", []string{"*"}, []string{"*"})

	_ = vaultStore.Put("user-42", "github", vault.Token{
		AccessToken: "ghp_test_token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	_ = vaultStore.Put("user-42", "stripe", vault.Token{
		AccessToken: "sk_test_token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	signerStore, err := signer.NewStore(database, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := signerStore.LoadOrCreateActive(1); err != nil {
		t.Fatal(err)
	}
	ledger := receipt.NewLedger(database, signerStore)

	// OAuth.
	oauthHandler := oauth.NewCallbackHandler(map[string]*oauth.Provider{
		"github": {
			Name:         "github",
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			AuthorizeURL: "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			Scopes:       []string{"repo"},
		},
	}, vaultStore, masterKey, "http://localhost:8080", nil)

	// Admin.
	adminHandler := admin.NewHandler(keyStore, oauthHandler, vaultStore, "admin-secret", nil)

	// Gateway — real dependencies throughout, matching production wiring.
	gw := gateway.New(gateway.Config{
		Registry:   reg,
		Vault:      vaultStore,
		Authorizer: keyStore,
		Receipts:   ledger,
		Limiter:    ratelimit.New(nil),
	})

	// Build combined mux.
	mux := http.NewServeMux()

	// Gateway routes — delegate to the gateway's ServeHTTP.
	mux.Handle("/healthz", gw)
	mux.Handle("/v1/", gw)
	mux.HandleFunc("GET /v1/receipts/pubkey", signer.PubkeyHandler(signerStore))

	// Admin routes.
	mux.HandleFunc("POST /admin/keys", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Secret") != "admin-secret" {
			w.WriteHeader(401)
			return
		}
		adminHandler.CreateKey(w, r)
	})
	mux.HandleFunc("POST /admin/link", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Secret") != "admin-secret" {
			w.WriteHeader(401)
			return
		}
		adminHandler.LinkAccount(w, r)
	})

	// OAuth callback.
	mux.HandleFunc("GET /auth/callback/{service}", oauthHandler.ServeHTTP)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts, database, apiKey
}

func TestIntegration_Healthz(t *testing.T) {
	ts, _, _ := setupIntegration(t, "")
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}

func TestIntegration_ActUnauthorized(t *testing.T) {
	ts, _, _ := setupIntegration(t, "")
	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-42"}`
	resp, err := http.Post(ts.URL+"/v1/act", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestIntegration_ActSuccess(t *testing.T) {
	// Mock upstream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_test_token" {
			t.Errorf("upstream auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":1,"name":"repo-1"}]`)
	}))
	defer upstream.Close()

	ts, database, apiKey := setupIntegration(t, upstream.URL)

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-42"}`
	req, _ := http.NewRequest("POST", ts.URL+"/v1/act", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(respBody))
	}

	// The action attempt must have committed exactly one receipt
	// (LEDG-04), signed and chained from an empty ledger (LEDG-06).
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM receipts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("receipts row count = %d, want 1", count)
	}
	var seq int64
	var policyDecision string
	if err := database.QueryRow("SELECT seq, policy_decision FROM receipts LIMIT 1").Scan(&seq, &policyDecision); err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}
	if policyDecision != "allow" {
		t.Fatalf("policy_decision = %s, want allow", policyDecision)
	}
}

// TestIntegration_ConcurrentActsProduceGapFreeChain fires concurrent real
// HTTP /v1/act calls end to end (gateway HTTP handler through to SQLite)
// and asserts the resulting receipt chain has no gaps or duplicates. The
// exhaustive 100-at-once proof lives in internal/receipt's own ledger
// test; this test is the end-to-end proof that the gateway's HTTP path
// reaches the same guarantee (LEDG-09).
func TestIntegration_ConcurrentActsProduceGapFreeChain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":1,"name":"repo-1"}]`)
	}))
	defer upstream.Close()

	ts, database, apiKey := setupIntegration(t, upstream.URL)

	const n = 25
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := `{"service":"github","action":"list_repos","on_behalf_of":"user-42"}`
			req, _ := http.NewRequest("POST", ts.URL+"/v1/act", bytes.NewReader([]byte(body)))
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("request %d: %v", i, err)
				return
			}
			defer resp.Body.Close()
			statuses[i] = resp.StatusCode
			io.Copy(io.Discard, resp.Body)
		}(i)
	}
	wg.Wait()

	for i, status := range statuses {
		if status != 200 {
			t.Fatalf("request %d: status = %d, want 200", i, status)
		}
	}

	rows, err := database.Query("SELECT seq FROM receipts ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	if len(seqs) != n {
		t.Fatalf("committed receipts = %d, want %d", len(seqs), n)
	}
	for i, seq := range seqs {
		if seq != int64(i+1) {
			t.Fatalf("seqs[%d] = %d, want %d (gap or duplicate)", i, seq, i+1)
		}
	}
}

func TestIntegration_AdminCreateKey(t *testing.T) {
	ts, _, _ := setupIntegration(t, "")

	body := `{"name":"new-agent","allowed_services":["stripe"],"allowed_users":["user-99"]}`
	req, _ := http.NewRequest("POST", ts.URL+"/admin/keys", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Secret", "admin-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if _, ok := result["key"]; !ok {
		t.Fatal("response missing 'key' field")
	}
}

func TestIntegration_AdminLinkAccount(t *testing.T) {
	ts, _, _ := setupIntegration(t, "")

	body := `{"user_id":"user-99","service":"github"}`
	req, _ := http.NewRequest("POST", ts.URL+"/admin/link", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Secret", "admin-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, string(respBody))
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["authorize_url"] == "" {
		t.Fatal("missing authorize_url")
	}
}

func TestIntegration_RateLimiting(t *testing.T) {
	configs := map[string]ratelimit.Config{
		"stripe": {RequestsPerSecond: 1, Burst: 1},
	}
	limiter := ratelimit.New(configs)

	// First should pass.
	if !limiter.Allow("agent-1", "stripe") {
		t.Fatal("first should pass")
	}
	// Second should fail.
	if limiter.Allow("agent-1", "stripe") {
		t.Fatal("second should be rate limited")
	}
}
