package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func testSetup(t *testing.T) (*Server, *vault.MemoryStore) {
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

	srv := New(Config{
		Registry:  reg,
		Vault:     store,
		AgentKeys: map[string]string{"test-key": "test-agent"},
	})

	return srv, store
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	srv, _ := testSetup(t)

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
	srv, _ := testSetup(t)

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
	srv, _ := testSetup(t)

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
	srv, _ := testSetup(t)

	req := httptest.NewRequest("GET", "/v1/services/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAct_Unauthorized(t *testing.T) {
	t.Parallel()
	srv, _ := testSetup(t)

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAct_MissingFields(t *testing.T) {
	t.Parallel()
	srv, _ := testSetup(t)

	body := `{"service":"github"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestAct_NoToken(t *testing.T) {
	t.Parallel()
	srv, _ := testSetup(t)

	body := `{"service":"github","action":"list_repos","on_behalf_of":"user-no-token"}`
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
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

	// Set up with upstream URL.
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

	srv := New(Config{
		Registry:  reg,
		Vault:     store,
		AgentKeys: map[string]string{"test-key": "test-agent"},
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
		Registry:  reg,
		Vault:     store,
		AgentKeys: map[string]string{"k": "agent"},
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
		name     string
		base     string
		path     string
		params   map[string]interface{}
		want     string
	}{
		{"no params", "https://api.example.com", "/users", nil, "https://api.example.com/users"},
		{"path params", "https://api.example.com", "/repos/{owner}/{repo}", map[string]interface{}{"owner": "octocat", "repo": "hello"}, "https://api.example.com/repos/octocat/hello"},
		{"trailing slash", "https://api.example.com/", "/users", nil, "https://api.example.com/users"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildURL(tc.base, tc.path, tc.params)
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
