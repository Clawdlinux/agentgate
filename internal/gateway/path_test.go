package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func TestBuildURLPathSegments(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"traversal", "../../admin/users", "..%2F..%2Fadmin%2Fusers"},
		{"slash", "owner/repo", "owner%2Frepo"},
		{"query", "repo?admin=true", "repo%3Fadmin=true"},
		{"fragment", "repo#admin", "repo%23admin"},
		{"encoded dots", "%2e%2e", "%252e%252e"},
		{"encoded traversal", "%2E%2E%2Fadmin", "%252E%252E%252Fadmin"},
		{"space", "hello world", "hello%20world"},
		{"unicode", "café/東京", "caf%C3%A9%2F%E6%9D%B1%E4%BA%AC"},
		{"large integer", json.Number("18446744073709551616"), "18446744073709551616"},
		{"negative integer", json.Number("-9007199254740993"), "-9007199254740993"},
		{"integer exponent", json.Number("1.00e+2"), "1.00e+2"},
		{"integer decimal", json.Number("42.000"), "42.000"},
		{"integer negative exponent", json.Number("1200e-2"), "1200e-2"},
		{"integral float", float64(42), "42"},
		{"integral float32", float32(42), "42"},
		{"integer", int(42), "42"},
		{"signed integer", int64(-42), "-42"},
		{"unsigned integer", uint64(18446744073709551615), "18446744073709551615"},
		{"boolean", false, "false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildURL("https://api.example.com/v1///", "/repos/{repo}", map[string]any{"repo": tc.value})
			if err != nil {
				t.Fatal(err)
			}
			if want := "https://api.example.com/v1/repos/" + tc.want; got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestBuildURLRejectsQueryPlaceholders(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/search?q={q}", "/repos/{repo}?q={q}"} {
		t.Run(path, func(t *testing.T) {
			got, err := buildURL("https://api.example.com", path, map[string]any{
				"repo": "hello",
				"q":    "hello&admin=true+extra",
			})
			if err == nil || got != "" {
				t.Fatalf("query placeholder accepted: %q, error=%v", got, err)
			}
		})
	}
}

func TestBuildURLDoesNotResubstitute(t *testing.T) {
	t.Parallel()
	for i := 0; i < 100; i++ {
		params := map[string]any{"repo": "{owner}", "owner": "octocat"}
		got, err := buildURL("https://api.example.com", "/repos/{owner}/{repo}/{repo}", params)
		if err != nil {
			t.Fatal(err)
		}
		const want = "https://api.example.com/repos/octocat/%7Bowner%7D/%7Bowner%7D"
		if got != want {
			t.Fatalf("iteration %d: got %q, want %q", i, got, want)
		}
	}
}

func TestBuildURLRejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		base   string
		path   string
		params map[string]any
	}{
		{"missing", "https://api.example.com", "/repos/{repo}", nil},
		{"empty", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": ""}},
		{"dot", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": "."}},
		{"dot dot", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": ".."}},
		{"null", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": nil}},
		{"object", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": map[string]any{"id": "repo"}}},
		{"array", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": []any{"repo"}}},
		{"fraction", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": json.Number("1.5")}},
		{"rounded fraction", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": json.Number("9007199254740993.1")}},
		{"tiny fraction", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": json.Number("1e-1000")}},
		{"float fraction", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": 1.5}},
		{"nan", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": math.NaN()}},
		{"infinity", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": math.Inf(1)}},
		{"invalid number", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": json.Number("1/2")}},
		{"non JSON number", "https://api.example.com", "/repos/{repo}", map[string]any{"repo": json.Number("0xff")}},
		{"unclosed placeholder", "https://api.example.com", "/repos/{repo", map[string]any{"repo": "hello"}},
		{"empty placeholder", "https://api.example.com", "/repos/{}", map[string]any{"": "hello"}},
		{"host change", "https://api.example.com", ".evil.example/repos", nil},
		{"base escape error", "https://api.example.com/%zz", "/repos", nil},
		{"final escape error", "https://api.example.com", "/repos/%zz", nil},
		{"relative base", "/api", "/repos", nil},
		{"opaque base", "https:api.example.com", "/repos", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := buildURL(tc.base, tc.path, tc.params); err == nil || got != "" {
				t.Fatalf("invalid URL accepted: %q, error=%v", got, err)
			}
		})
	}
}

func TestBuildURLShippedTemplates(t *testing.T) {
	t.Parallel()
	var templates int
	for _, root := range []string{"../../config", "../../configs"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
				return nil
			}
			reg := registry.New()
			if err := reg.LoadFile(path); err != nil {
				return err
			}
			for _, name := range reg.List() {
				svc, err := reg.Get(name)
				if err != nil {
					return err
				}
				for actionName, action := range svc.Actions {
					templates++
					t.Run(path+"/"+name+"/"+actionName, func(t *testing.T) {
						got, err := buildURL(svc.BaseURL, action.Path, map[string]any{
							"owner": "octocat",
							"repo":  "hello-world",
						})
						if err != nil {
							t.Fatal(err)
						}
						wantPath := strings.NewReplacer("{owner}", "octocat", "{repo}", "hello-world").Replace(action.Path)
						if strings.ContainsAny(wantPath, "{}") {
							t.Fatalf("add representative params for template %q", action.Path)
						}
						if want := strings.TrimRight(svc.BaseURL, "/") + wantPath; got != want {
							t.Fatalf("got %q, want %q", got, want)
						}
					})
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if templates == 0 {
		t.Fatal("no shipped templates checked")
	}
	t.Logf("checked %d shipped templates", templates)
}

func TestActPathParamsStayEscapedOnWire(t *testing.T) {
	cases := []struct {
		name   string
		params string
		want   string
	}{
		{"traversal", `{"repo":"../../admin/users"}`, "/repos/..%2F..%2Fadmin%2Fusers"},
		{"slash", `{"repo":"owner/repo"}`, "/repos/owner%2Frepo"},
		{"delimiters", `{"repo":"repo?admin=true#fragment"}`, "/repos/repo%3Fadmin=true%23fragment"},
		{"encoded dots", `{"repo":"%2e%2e"}`, "/repos/%252e%252e"},
		{"placeholder", `{"repo":"{owner}","owner":"octocat"}`, "/repos/%7Bowner%7D"},
		{"unicode", `{"repo":"café 東京"}`, "/repos/caf%C3%A9%20%E6%9D%B1%E4%BA%AC"},
		{"large number", `{"repo":9007199254740993}`, "/repos/9007199254740993"},
		{"boolean", `{"repo":false}`, "/repos/false"},
		{"unused structured params", `{"repo":"hello","body":{"labels":["bug"]},"optional":null}`, "/repos/hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.RequestURI != tc.want || r.URL.RawQuery != "" || r.URL.Fragment != "" {
					t.Errorf("unexpected upstream target: %q", r.RequestURI)
				}
				if r.Header.Get("Authorization") != "Bearer injected-secret" {
					t.Error("upstream credential missing")
				}
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer upstream.Close()
			srv, receipts := transportGateway(t, upstream.URL, "{}")
			if err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth: {type: bearer}
    actions:
      list_repos: {method: GET, path: "/repos/{repo}"}
`, upstream.URL))); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
				`{"service":"github","action":"list_repos","on_behalf_of":"user-1","params":`+tc.params+`}`))
			req.Header.Set("Authorization", "Bearer test-key")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != http.StatusOK || calls.Load() != 1 {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, calls.Load(), w.Body)
			}
			if receipts.count() != 1 {
				t.Fatalf("receipts=%d, want 1", receipts.count())
			}
		})
	}
}

func TestActInvalidPathParamsAreReceipted(t *testing.T) {
	cases := []struct {
		name   string
		params string
	}{
		{"missing", `{}`},
		{"empty", `{"repo":""}`},
		{"dot", `{"repo":"."}`},
		{"dot dot", `{"repo":".."}`},
		{"null", `{"repo":null}`},
		{"object", `{"repo":{"id":"hello"}}`},
		{"array", `{"repo":["hello"]}`},
		{"fraction", `{"repo":1.5}`},
		{"rounded fraction", `{"repo":9007199254740993.1}`},
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer upstream.Close()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, receipts := transportGateway(t, upstream.URL, "{}")
			if err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth: {type: bearer}
    actions:
      list_repos: {method: GET, path: "/repos/{repo}"}
`, upstream.URL))); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
				`{"service":"github","action":"list_repos","on_behalf_of":"user-1","params":`+tc.params+`}`))
			req.Header.Set("Authorization", "Bearer test-key")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			var body ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || body.Code != "bad_request" || body.Error != "invalid params" {
				t.Errorf("status=%d body=%s", w.Code, w.Body)
			}
			if calls.Load() != 0 {
				t.Errorf("upstream calls=%d, want 0", calls.Load())
			}
			if receipts.count() != 1 {
				t.Fatalf("receipts=%d, want 1", receipts.count())
			}
			draft := receipts.last()
			hash, err := receipt.DigestParams([]byte(tc.params))
			if err != nil {
				t.Fatal(err)
			}
			if draft.StatusCode != 400 || draft.Error != "bad_request" || draft.PolicyDecision != "deny" || draft.ParamsSHA256 != hash {
				t.Fatalf("unexpected receipt: %+v", draft)
			}
		})
	}
}

type pathCountingVault struct {
	vault.Store
	gets atomic.Int32
	puts atomic.Int32
}

func (v *pathCountingVault) Get(userID, service string) (vault.Token, error) {
	v.gets.Add(1)
	return v.Store.Get(userID, service)
}

func (v *pathCountingVault) Put(userID, service string, tok vault.Token) error {
	v.puts.Add(1)
	return v.Store.Put(userID, service, tok)
}

func TestActInvalidPathParamsBeforeVault(t *testing.T) {
	for _, hasToken := range []bool{false, true} {
		t.Run(fmt.Sprintf("has_token=%t", hasToken), func(t *testing.T) {
			srv, store, receipts := testSetup(t)
			if hasToken {
				if err := store.Put("user-1", "github", vault.Token{
					AccessToken:  "expiring-token",
					RefreshToken: "refresh-token",
					ExpiresAt:    time.Now().Add(time.Minute),
				}); err != nil {
					t.Fatal(err)
				}
			}
			counted := &pathCountingVault{Store: store}
			srv.cfg.Vault = counted
			var refreshCalls atomic.Int32
			srv.cfg.Refreshers = map[string]vault.RefreshFunc{
				"github": func(context.Context, string) (string, string, time.Duration, error) {
					refreshCalls.Add(1)
					return "new-token", "new-refresh", time.Hour, nil
				},
			}
			req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
				`{"service":"github","action":"create_issue","on_behalf_of":"user-1","params":{"owner":"octocat","repo":".."}}`))
			req.Header.Set("Authorization", "Bearer test-key")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			var body ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusBadRequest || body.Code != "bad_request" || body.Error != "invalid params" {
				t.Errorf("status=%d body=%s", w.Code, w.Body)
			}
			if counted.gets.Load() != 0 || counted.puts.Load() != 0 || refreshCalls.Load() != 0 {
				t.Errorf("vault gets=%d puts=%d refresh calls=%d, want all 0",
					counted.gets.Load(), counted.puts.Load(), refreshCalls.Load())
			}
			if receipts.count() != 1 {
				t.Fatalf("receipts=%d, want 1", receipts.count())
			}
			draft := receipts.last()
			if draft.StatusCode != 400 || draft.Error != "bad_request" || draft.PolicyDecision != "deny" {
				t.Fatalf("unexpected receipt: %+v", draft)
			}
		})
	}
}

func TestActInvalidBaseURLIsInternal(t *testing.T) {
	for _, base := range []string{
		"https://operator-private-value:private-password@api.example.com/%zz",
		"/relative-base",
		"https:opaque-base",
	} {
		t.Run(base, func(t *testing.T) {
			srv, store, receipts := testSetup(t)
			if err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %q
    actions:
      list_repos: {method: GET, path: /repos}
`, base))); err != nil {
				t.Fatal(err)
			}
			if err := store.Put("user-1", "github", vault.Token{AccessToken: "access-token"}); err != nil {
				t.Fatal(err)
			}
			counted := &pathCountingVault{Store: store}
			srv.cfg.Vault = counted
			w := transportAct(t, srv)
			var body ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusInternalServerError || body.Code != "internal" || body.Error != "failed to build upstream request" {
				t.Errorf("status=%d body=%s", w.Code, w.Body)
			}
			if counted.gets.Load() != 0 || counted.puts.Load() != 0 {
				t.Errorf("vault gets=%d puts=%d, want both 0", counted.gets.Load(), counted.puts.Load())
			}
			if receipts.count() != 1 {
				t.Fatalf("receipts=%d, want 1", receipts.count())
			}
			draft := receipts.last()
			if draft.StatusCode != 500 || draft.Error != "internal" || draft.PolicyDecision != "allow" {
				t.Fatalf("unexpected receipt: %+v", draft)
			}
		})
	}
}
