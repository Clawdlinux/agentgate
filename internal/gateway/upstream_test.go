package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/upstream"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func transportGateway(t *testing.T, url, settings string) (*Server, *fakeReceipts) {
	t.Helper()
	srv, store, receipts := testSetup(t)
	err := srv.cfg.Registry.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: %s
    auth: {type: bearer}
    upstream: %s
    actions:
      list_repos: {method: GET, path: /resource}
`, url, settings)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("user-1", "github", vault.Token{AccessToken: "injected-secret"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.CloseIdleConnections)
	return srv, receipts
}

func transportAct(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
		`{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestAct_BreakerOpenIsReceipted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer server.Close()
	srv, receipts := transportGateway(t, server.URL, "{breaker: {failure_threshold: 2, cooldown: 30s}}")
	for i := 0; i < 2; i++ {
		if w := transportAct(t, srv); w.Code != 500 {
			t.Fatalf("status=%d", w.Code)
		}
	}
	w := transportAct(t, srv)
	if w.Code != 503 || calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d", w.Code, calls.Load())
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "upstream_unavailable" {
		t.Fatalf("code=%s", body.Code)
	}
	draft := receipts.last()
	if receipts.count() != 3 || draft.StatusCode != 503 || draft.PolicyDecision != "allow" || draft.Error != "upstream_unavailable" {
		t.Fatalf("count=%d draft=%+v", receipts.count(), draft)
	}
}

func TestAct_RetryProducesOneFinalReceipt(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer injected-secret" {
			t.Error("credential missing from retry")
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	srv, receipts := transportGateway(t, server.URL, "{}")
	w := transportAct(t, srv)
	if w.Code != 200 || calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d", w.Code, calls.Load())
	}
	if receipts.count() != 1 || receipts.last().StatusCode != 200 {
		t.Fatalf("count=%d draft=%+v", receipts.count(), receipts.last())
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("https://private-host/path?access_token=injected-secret")
}

func TestAct_TransportErrorsAreRedacted(t *testing.T) {
	srv, receipts := transportGateway(t, "http://private-host", "{retry: {max_retries: 0}}")
	var logs bytes.Buffer
	srv.logger = slog.New(slog.NewTextHandler(&logs, nil))
	srv.upstreams = upstream.New(upstream.Options{Client: &http.Client{Transport: errorTransport{}}})
	w := transportAct(t, srv)
	if w.Code != 502 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "private-host") || strings.Contains(w.Body.String(), "injected-secret") {
		t.Fatalf("error leaked details: %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), `"error":"upstream request failed"`) {
		t.Fatalf("error is not fixed: %s", w.Body)
	}
	if !strings.Contains(logs.String(), "upstream request failed") || strings.Contains(logs.String(), "injected-secret") {
		t.Fatalf("unsafe or missing error log: %s", &logs)
	}
	if receipts.count() != 1 || receipts.last().Error != "upstream_error" {
		t.Fatalf("receipt=%+v", receipts.last())
	}
}

func TestAct_UpstreamDeadlineIsReceipted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	srv, receipts := transportGateway(t, server.URL, "{request_timeout: 20ms}")
	w := transportAct(t, srv)
	if w.Code != 502 || receipts.count() != 1 || receipts.last().Error != "upstream_error" {
		t.Fatalf("status=%d count=%d draft=%+v", w.Code, receipts.count(), receipts.last())
	}
}

func TestAct_CanceledRequestStillCommitsReceipt(t *testing.T) {
	srv, receipts := transportGateway(t, "http://unused", "{}")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(
		`{"service":"github","action":"list_repos","on_behalf_of":"user-1"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-key")
	start := time.Now()
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 502 || receipts.count() != 1 || time.Since(start) > time.Second {
		t.Fatalf("status=%d receipts=%d", w.Code, receipts.count())
	}
}
