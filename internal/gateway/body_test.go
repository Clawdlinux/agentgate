package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	upstreamclient "github.com/Clawdlinux/agentgate/internal/upstream"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func bodyGateway(t *testing.T, upstreamURL, settings string, stream bool) (*Server, *fakeReceipts) {
	t.Helper()
	srv, receipts := transportGateway(t, upstreamURL, settings)
	err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth: {type: bearer}
    upstream: %s
    actions:
      list_repos: {method: GET, path: /resource, stream: %t}
`, upstreamURL, settings, stream)))
	if err != nil {
		t.Fatal(err)
	}
	return srv, receipts
}

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type countedResponseBody struct {
	reader io.Reader
	read   int
	closed bool
}

func (b *countedResponseBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *countedResponseBody) Close() error { b.closed = true; return nil }

type brokenResponseReader struct{}

func (brokenResponseReader) Read([]byte) (int, error) { return 0, errors.New("body read failed") }

func TestAct_ResponseReadBoundAndClose(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		reader     io.Reader
		read       int
		status     int
	}{
		{"oversize", "upstream_response_too_large", strings.NewReader(strings.Repeat("x", 1<<20)), 1025, 502},
		{"exact", "", strings.NewReader(strings.Repeat("x", 1024)), 1024, 200},
		{"read error", "upstream_error", io.MultiReader(strings.NewReader("partial"), brokenResponseReader{}), 7, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &countedResponseBody{reader: tc.reader}
			srv, receipts := bodyGateway(t, "http://unused", "{max_response_bytes: 1024}", false)
			srv.upstreams = upstreamclient.New(upstreamclient.Options{Client: &http.Client{
				Transport: responseTransport(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
				}),
			}})
			w := transportAct(t, srv)
			if w.Code != tc.status || body.read != tc.read || !body.closed {
				t.Fatalf("status=%d read=%d closed=%t", w.Code, body.read, body.closed)
			}
			if receipts.count() != 1 || receipts.last().Error != tc.code {
				t.Fatalf("count=%d draft=%+v", receipts.count(), receipts.last())
			}
		})
	}
}

func TestAct_PreservesRawParamsBodyAndURL(t *testing.T) {
	const params = `{ "id": 9007199254740993, "nested": [1.00e+2, "😀", null] }`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/items/9007199254740993" {
			t.Errorf("path=%s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != params {
			t.Errorf("body=%s error=%v", body, err)
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer up.Close()
	srv, receipts := bodyGateway(t, up.URL, "{}", false)
	if err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth: {type: bearer}
    actions:
      list_repos: {method: POST, path: "/items/{id}"}
`, up.URL))); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
		`{"service":"github","action":"list_repos","on_behalf_of":"user-1","params":`+params+`}`))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 || receipts.count() != 1 {
		t.Fatalf("status=%d count=%d", w.Code, receipts.count())
	}
}

func TestAct_ResponseBodyLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings string
		size     int
		status   int
	}{
		{"default oversize", "{}", (1 << 20) + 1, 502},
		{"default exact", "{}", 1 << 20, 200},
		{"override oversize", "{max_response_bytes: 1024}", 1025, 502},
		{"override exact", "{max_response_bytes: 1024}", 1024, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := strings.Repeat("x", tc.size)
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, content)
			}))
			defer up.Close()
			srv, receipts := bodyGateway(t, up.URL, tc.settings, false)
			w := transportAct(t, srv)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			if receipts.count() != 1 || receipts.last().StatusCode != tc.status {
				t.Fatalf("receipts=%d draft=%+v", receipts.count(), receipts.last())
			}
			if tc.status == 502 {
				var body ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Code != "upstream_response_too_large" ||
					body.Error != "upstream response exceeds configured limit" ||
					receipts.last().Error != body.Code {
					t.Fatalf("body=%+v receipt=%+v", body, receipts.last())
				}
			} else {
				var body ActResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.BodyText != content {
					t.Fatalf("body size=%d error=%v", len(body.BodyText), err)
				}
			}
		})
	}
}

func TestAct_ResponseBodyShape(t *testing.T) {
	for _, content := range []string{
		`{"nested":[1,true,null,{"unicode":"😀"}]}`, `[1,"two"]`, `null`, `"string"`, `1e3`,
		"plain text", `{"broken":`, "",
	} {
		t.Run(content, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, content)
			}))
			defer up.Close()
			srv, _ := bodyGateway(t, up.URL, "{}", false)
			w := transportAct(t, srv)
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if json.Valid([]byte(content)) {
				if string(body["body"]) != content || body["body_text"] != nil {
					t.Fatalf("response=%s", w.Body)
				}
			} else {
				var text string
				if value := body["body_text"]; value != nil {
					if err := json.Unmarshal(value, &text); err != nil {
						t.Fatal(err)
					}
				}
				if text != content || body["body"] != nil {
					t.Fatalf("response=%s", w.Body)
				}
			}
		})
	}
}

type benchmarkReceipts struct {
	draft receipt.Draft
}

func (f *benchmarkReceipts) Append(_ context.Context, d receipt.Draft) (receipt.Receipt, error) {
	f.draft = d
	return receipt.Receipt{Seq: 1, StatusCode: d.StatusCode}, nil
}

func BenchmarkHandleAct(b *testing.B) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, `{"ok":true,"items":[1,2,3]}`)
	}))
	defer up.Close()
	reg := registry.New()
	if err := reg.LoadBytes([]byte(fmt.Sprintf(`
services:
  example:
    base_url: %s
    auth: {type: bearer}
    actions:
      create: {method: POST, path: "/items/{id}"}
`, up.URL))); err != nil {
		b.Fatal(err)
	}
	store, err := vault.NewMemoryStore(make([]byte, 32))
	if err != nil {
		b.Fatal(err)
	}
	if err := store.Put("user", "example", vault.Token{AccessToken: "token"}); err != nil {
		b.Fatal(err)
	}
	srv := New(Config{
		Registry: reg, Vault: store, Receipts: &benchmarkReceipts{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authorizer: &fakeAuthorizer{keys: map[string]*auth.AgentKey{
			"bench": {ID: "agent", AllowedServices: []string{"*"}, AllowedUsers: []string{"*"}},
		}},
	})
	defer srv.CloseIdleConnections()
	const body = `{"service":"example","action":"create","on_behalf_of":"user","params":{"id":9007199254740993,"name":"😀","nested":{"b":2,"a":[null,true,1.25]}}}`
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer bench")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != 200 {
			b.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
}
