package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Clawdlinux/agentgate/internal/admin"
	"github.com/Clawdlinux/agentgate/internal/audit"
	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/gateway"
	"github.com/Clawdlinux/agentgate/internal/oauth"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func setupIntegration(t *testing.T, upstreamURL string) (*httptest.Server, *sql.DB, string) {
	t.Helper()

	// In-memory SQLite.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Run migrations.
	_, err = db.Exec(`
		CREATE TABLE agent_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL,
			allowed_services TEXT NOT NULL DEFAULT '["*"]',
			allowed_users TEXT NOT NULL DEFAULT '["*"]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			revoked_at DATETIME
		);
		CREATE TABLE tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			service TEXT NOT NULL,
			access_token_enc BLOB NOT NULL,
			refresh_token_enc BLOB,
			expires_at DATETIME,
			scopes TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, service)
		);
		CREATE TABLE audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			agent_key_id TEXT NOT NULL,
			service TEXT NOT NULL,
			action TEXT NOT NULL,
			user_id TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			error TEXT
		);
	`)
	if err != nil {
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

	// Vault.
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i)
	}
	store, _ := vault.NewMemoryStore(encKey)

	// Key store (for admin handler).
	keyStore := auth.NewKeyStore(db)

	// Generate an API key and store plaintext in gateway's AgentKeys map.
	_, apiKey, _ := keyStore.Create(t.Context(), "test-agent", []string{"*"}, []string{"*"})

	// Store a token for testing.
	_ = store.Put("user-42", "github", vault.Token{
		AccessToken: "ghp_test_token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	_ = store.Put("user-42", "stripe", vault.Token{
		AccessToken: "sk_test_token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

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
	}, store, encKey, "http://localhost:8080", nil)

	// Admin.
	adminHandler := admin.NewHandler(keyStore, oauthHandler, store, "admin-secret", nil)

	// Audit.
	auditLogger := audit.NewLogger(db, nil)
	t.Cleanup(func() { auditLogger.Close() })

	// Gateway — the AgentKeys map uses plaintext key as the map key.
	gw := gateway.New(gateway.Config{
		Registry:  reg,
		Vault:     store,
		AgentKeys: map[string]string{apiKey: "test-agent"},
	})

	// Build combined mux.
	mux := http.NewServeMux()

	// Gateway routes — delegate to the gateway's ServeHTTP.
	mux.Handle("/healthz", gw)
	mux.Handle("/v1/", gw)

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

	return ts, db, apiKey
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

	ts, _, apiKey := setupIntegration(t, upstream.URL)

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
