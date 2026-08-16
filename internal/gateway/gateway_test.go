package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

// fakeAuthorizer is a small in-memory AgentAuthorizer for unit tests, so
// tests do not need to open SQLite (per ARCHITECTURE.md's dependency
// injection guidance).
type fakeAuthorizer struct {
	keys map[string]*auth.AgentKey // plaintext -> key
}

func (f *fakeAuthorizer) Validate(_ context.Context, plaintext string) (*auth.AgentKey, error) {
	k, ok := f.keys[plaintext]
	if !ok {
		return nil, auth.ErrKeyNotFound
	}
	return k, nil
}

// fakeReceipts is a small in-memory ReceiptRecorder for unit tests. It can
// be told to fail the next Append to exercise LEDG-07 at the gateway
// level: a failed receipt must return no successful action response.
type fakeReceipts struct {
	mu       sync.Mutex
	seq      uint64
	drafts   []receipt.Draft
	failNext bool
}

func (f *fakeReceipts) Append(_ context.Context, d receipt.Draft) (receipt.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return receipt.Receipt{}, errors.New("forced append failure")
	}
	f.seq++
	f.drafts = append(f.drafts, d)
	return receipt.Receipt{Seq: f.seq, PolicyDecision: d.PolicyDecision, StatusCode: d.StatusCode}, nil
}

func (f *fakeReceipts) last() receipt.Draft {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drafts[len(f.drafts)-1]
}

func (f *fakeReceipts) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.drafts)
}

func testSetup(t *testing.T) (*Server, *vault.MemoryStore, *fakeReceipts) {
	t.Helper()

	reg := registry.New()
	_ = reg.LoadBytes([]byte(`
services:
  github:
    base_url: PLACEHOLDER
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
    base_url: PLACEHOLDER
    auth:
      type: bearer
    actions:
      list_invoices:
        method: GET
        path: /invoices
`))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, _ := vault.NewMemoryStore(key)

	receipts := &fakeReceipts{}
	srv := New(Config{
		Registry: reg,
		Vault:    store,
		Authorizer: &fakeAuthorizer{keys: map[string]*auth.AgentKey{
			"test-key": {ID: "test-agent", AllowedServices: []string{"*"}, AllowedUsers: []string{"*"}},
		}},
		Receipts: receipts,
	})

	return srv, store, receipts
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	srv, _, _ := testSetup(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("status = %s", body["status"])
	}
}

func TestListServices(t *testing.T) {
	t.Parallel()
	srv, _, _ := testSetup(t)

	req := httptest.NewRequest("GET", "/v1/services", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string][]string
	json.NewDecoder(w.Body).Decode(&body)
	if len(body["services"]) != 2 {
		t.Fatalf("services = %v", body["services"])
	}
}

func TestDescribeService(t *testing.T) {
	t.Parallel()
	srv, _, _ := testSetup(t)

	req := httptest.NewRequest("GET", "/v1/services/github", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["name"] != "github" {
		t.Fatalf("name = %v", body["name"])
	}
}

func TestDescribeService_NotFound(t *testing.T) {
	t.Parallel()
	srv, _, _ := testSetup(t)

	req := httptest.NewRequest("GET", "/v1/services/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestAct_Unauthorized covers the receipted-coverage boundary: an unknown
// API key never reaches a receipt.
func TestAct_Unauthorized(t *testing.T) {
	t.Parallel()
	srv, _, receipts := testSetup(t)

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if receipts.count() != 0 {
		t.Fatalf("receipts = %d, want 0 for an unauthenticated request", receipts.count())
	}
}

// TestAct_MissingFields covers the other half of the receipted-coverage
// boundary: a malformed request body never reaches a receipt either.
func TestAct_MissingFields(t *testing.T) {
	t.Parallel()
	srv, _, receipts := testSetup(t)

	body := `{"service":"github"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if receipts.count() != 0 {
		t.Fatalf("receipts = %d, want 0 for a malformed request", receipts.count())
	}
}

func TestAct_ScopeDenied(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	_ = reg.LoadBytes([]byte(`
services:
  github:
    base_url: PLACEHOLDER
    auth:
      type: oauth2
    actions:
      list_repos:
        method: GET
        path: /user/repos
`))
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, _ := vault.NewMemoryStore(key)
	receipts := &fakeReceipts{}
	srv := New(Config{
		Registry: reg,
		Vault:    store,
		Authorizer: &fakeAuthorizer{keys: map[string]*auth.AgentKey{
			"scoped-key": {ID: "scoped-agent", AllowedServices: []string{"stripe"}, AllowedUsers: []string{"*"}},
		}},
		Receipts: receipts,
	})

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer scoped-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if receipts.count() != 1 {
		t.Fatalf("receipts = %d, want 1 for a scope-denied attempt (LEDG-05)", receipts.count())
	}
	if got := receipts.last().PolicyDecision; got != "deny" {
		t.Fatalf("policy_decision = %s, want deny", got)
	}
}

func TestAct_RateLimited(t *testing.T) {
	t.Parallel()
	srv, _, receipts := testSetup(t)
	srv.cfg.Limiter = denyAllLimiter{}

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := receipts.last().PolicyDecision; got != "rate_limited" {
		t.Fatalf("policy_decision = %s, want rate_limited", got)
	}
}

type denyAllLimiter struct{}

func (denyAllLimiter) Allow(string, string) bool { return false }

func TestAct_NoToken(t *testing.T) {
	t.Parallel()
	srv, _, receipts := testSetup(t)

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-no-token"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if got := receipts.last().PolicyDecision; got != "allow" {
		t.Fatalf("policy_decision = %s, want allow (identity+policy allowed proceeding)", got)
	}
	if got := receipts.last().Error; got != "token_missing" {
		t.Fatalf("error = %s, want token_missing", got)
	}
}

// TestAct_ReceiptAppendFailure covers LEDG-07 at the gateway level: a
// failed receipt commit must return no successful action response.
func TestAct_ReceiptAppendFailure(t *testing.T) {
	t.Parallel()
	srv, store, receipts := testSetup(t)
	_ = store.Put("user-1", "github", vault.Token{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	receipts.failNext = true

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the receipt commit fails", w.Code)
	}
}

func TestAct_Success(t *testing.T) {
	t.Parallel()

	// Mock upstream SaaS API.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth was injected.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer user-github-token" {
			t.Errorf("upstream got auth = %q, want Bearer user-github-token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":1,"name":"repo-1"},{"id":2,"name":"repo-2"}]`)
	}))
	defer upstream.Close()

	reg := registry.New()
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
`, upstream.URL)))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, _ := vault.NewMemoryStore(key)
	_ = store.Put("user-1", "github", vault.Token{
		AccessToken: "user-github-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	receipts := &fakeReceipts{}
	srv := New(Config{
		Registry: reg,
		Vault:    store,
		Authorizer: &fakeAuthorizer{keys: map[string]*auth.AgentKey{
			"test-key": {ID: "test-agent", AllowedServices: []string{"*"}, AllowedUsers: []string{"*"}},
		}},
		Receipts: receipts,
	})

	actBody := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(actBody))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		body, _ := io.ReadAll(w.Body)
		t.Fatalf("status = %d, body = %s", w.Code, body)
	}

	var resp ActResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != 200 {
		t.Fatalf("upstream status = %d", resp.Status)
	}
	if resp.Body == nil {
		t.Fatal("body is nil")
	}
	if resp.LatencyMS < 0 {
		t.Fatalf("latency = %d", resp.LatencyMS)
	}
	if receipts.count() != 1 {
		t.Fatalf("receipts = %d, want 1", receipts.count())
	}
	if got := receipts.last().PolicyDecision; got != "allow" {
		t.Fatalf("policy_decision = %s, want allow", got)
	}
}

func TestAct_PathParams(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octocat/hello/issues" {
			t.Errorf("path = %s, want /repos/octocat/hello/issues", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":42,"title":"test issue"}`)
	}))
	defer upstream.Close()

	reg := registry.New()
	_ = reg.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth:
      type: oauth2
    actions:
      create_issue:
        method: POST
        path: /repos/{owner}/{repo}/issues
        params:
          owner: string
          repo: string
          title: string
`, upstream.URL)))

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, _ := vault.NewMemoryStore(key)
	_ = store.Put("user-1", "github", vault.Token{
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	srv := New(Config{
		Registry: reg,
		Vault:    store,
		Authorizer: &fakeAuthorizer{keys: map[string]*auth.AgentKey{
			"k": {ID: "agent", AllowedServices: []string{"*"}, AllowedUsers: []string{"*"}},
		}},
		Receipts: &fakeReceipts{},
	})

	actBody := `{"service":"github","action":"create_issue","on_behalf_of":"user-1","params":{"owner":"octocat","repo":"hello","title":"test issue"}}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(actBody))
	req.Header.Set("Authorization", "Bearer k")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		body, _ := io.ReadAll(w.Body)
		t.Fatalf("status = %d, body = %s", w.Code, body)
	}
}

func TestBuildURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		base   string
		path   string
		params string
		want   string
	}{
		{"no params", "https://api.example.com", "/users", ``, "https://api.example.com/users"},
		{"path params", "https://api.example.com", "/repos/{owner}/{repo}", `{"owner":"octocat","repo":"hello"}`, "https://api.example.com/repos/octocat/hello"},
		{"trailing slash", "https://api.example.com/", "/users", ``, "https://api.example.com/users"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildURL(tc.base, tc.path, json.RawMessage(tc.params))
			if err != nil {
				t.Fatalf("buildURL: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
