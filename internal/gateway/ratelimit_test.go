package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	upstreamclient "github.com/Clawdlinux/agentgate/internal/upstream"
)

type countingDelegation struct{ calls atomic.Int32 }

func (v *countingDelegation) Verify([]byte, string, string, string, string, time.Time) ([]string, error) {
	v.calls.Add(1)
	return nil, errors.New("denied")
}

func limitedAct(srv *Server, key, tenant, service string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(fmt.Sprintf(
		`{"service":%q,"action":"list_repos","on_behalf_of":%q}`, service, tenant)))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Agentgate-Delegation", "AA==")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestAct_EarlyRateLimitsAndReceipts(t *testing.T) {
	for _, scope := range []string{"key", "tenant", "service"} {
		t.Run(scope, func(t *testing.T) {
			now := time.Unix(1000, 0)
			settings := ratelimit.Settings{}
			configs := map[string]ratelimit.Config{}
			switch scope {
			case "key":
				settings.KeyRPS, settings.KeyBurst = 0.4, 1
			case "tenant":
				settings.TenantRPS, settings.TenantBurst = 0.4, 1
			case "service":
				configs["github"] = ratelimit.Config{RequestsPerSecond: 0.4, Burst: 1}
			}
			l := ratelimit.New(configs, ratelimit.WithSettings(settings),
				ratelimit.WithClock(func() time.Time { return now }))
			l.Check("test-agent", "human:alice", "github")
			srv, _, receipts := testSetup(t)
			srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"].AllowedUsers = []string{"alice"}
			srv.cfg.Limiter = l
			verifier := &countingDelegation{}
			srv.cfg.Delegation = verifier
			// Any registry, URL, or vault access would panic.
			srv.cfg.Registry, srv.cfg.Vault = nil, nil
			key := "test-key"
			if scope == "tenant" {
				key = "second-key"
				srv.cfg.Authorizer.(*fakeAuthorizer).keys[key] = &auth.AgentKey{
					ID: "second-agent", AllowedUsers: []string{"alice"}, AllowedServices: []string{"github"},
				}
			}
			w := limitedAct(srv, key, "alice", "github")
			if w.Code != 429 || w.Header().Get("Retry-After") != "3" ||
				!strings.Contains(w.Body.String(), `"code":"rate_limited"`) {
				t.Fatalf("status=%d retry=%q body=%s", w.Code, w.Header().Get("Retry-After"), w.Body)
			}
			if verifier.calls.Load() != 0 {
				t.Fatalf("delegation calls = %d", verifier.calls.Load())
			}
			draft := receipts.last()
			if receipts.count() != 1 || draft.StatusCode != 429 || draft.Error != "rate_limited" ||
				draft.PolicyDecision != "rate_limited" || draft.HumanPrincipal != "alice" {
				t.Fatalf("receipt = %+v", draft)
			}
			receipts.failNext = true
			w = limitedAct(srv, key, "alice", "github")
			if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"receipt_failed"`) ||
				w.Header().Get("Retry-After") != "" {
				t.Fatalf("failed receipt response = %d %s", w.Code, w.Body)
			}
			now = now.Add(2500 * time.Millisecond)
			w = limitedAct(srv, key, "alice", "github")
			if w.Code != 403 || verifier.calls.Load() != 1 {
				t.Fatalf("refill status=%d delegation calls=%d", w.Code, verifier.calls.Load())
			}
		})
	}
}

func TestAct_TenantConcurrencyAndSSE(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			entered := make(chan struct{}, 2)
			finish := make(chan struct{})
			var calls atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				entered <- struct{}{}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: first\n\n")
					w.(http.Flusher).Flush()
				}
				select {
				case <-finish:
					fmt.Fprint(w, "{}")
				case <-r.Context().Done():
				}
			}))
			defer up.Close()
			defer close(finish)
			srv, receipts := bodyGateway(t, up.URL, "{}", stream)
			srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"].AllowedUsers = []string{"user-1"}
			now := time.Unix(1000, 0)
			srv.cfg.Limiter = ratelimit.New(nil, ratelimit.WithSettings(ratelimit.Settings{TenantMaxInflight: 1}),
				ratelimit.WithClock(func() time.Time { return now }))
			srv.cfg.Authorizer.(*fakeAuthorizer).keys["second-key"] = &auth.AgentKey{
				ID: "second-agent", AllowedServices: []string{"*"}, AllowedUsers: []string{"user-1"},
			}
			firstDone := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Test-First") != "" {
					defer close(firstDone)
				}
				srv.ServeHTTP(w, r)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			type result struct {
				resp *http.Response
				err  error
			}
			resultCh := make(chan result, 1)
			go func() {
				req := streamRequest(ctx, server.URL, "text/event-stream")
				req.Header.Set("X-Test-First", "1")
				resp, err := server.Client().Do(req)
				resultCh <- result{resp, err}
			}()
			awaitSignal(t, entered)
			var first result
			if stream {
				first = <-resultCh
				if first.err != nil {
					t.Fatal(first.err)
				}
				defer first.resp.Body.Close()
			}
			req := streamRequest(ctx, server.URL, "text/event-stream")
			req.Header.Set("Authorization", "Bearer second-key")
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "1" ||
				!strings.Contains(string(body), `"code":"concurrency_limited"`) || calls.Load() != 1 {
				t.Fatalf("status=%d body=%s calls=%d err=%v", resp.StatusCode, body, calls.Load(), err)
			}
			if receipts.last().Error != "concurrency_limited" || receipts.last().PolicyDecision != "rate_limited" {
				t.Fatalf("receipt = %+v", receipts.last())
			}
			finish <- struct{}{}
			if !stream {
				first = <-resultCh
				if first.err != nil {
					t.Fatal(first.err)
				}
				defer first.resp.Body.Close()
			}
			if _, err := io.ReadAll(first.resp.Body); err != nil {
				t.Fatal(err)
			}
			awaitSignal(t, firstDone)
			if first.resp.StatusCode != 200 {
				t.Fatalf("first status = %d", first.resp.StatusCode)
			}
			release, delay := srv.cfg.Limiter.Acquire("human:user-1")
			if delay != 0 {
				t.Fatal("request completion did not release the tenant slot")
			}
			release()
			if receipts.count() != 2 {
				t.Fatalf("receipts = %d", receipts.count())
			}
		})
	}
}

func TestAct_ConcurrencyReleaseOnFailures(t *testing.T) {
	for _, kind := range []string{"upstream", "body", "receipt", "stream-receipt"} {
		t.Run(kind, func(t *testing.T) {
			srv, receipts := bodyGateway(t, "http://unused", "{}", kind == "stream-receipt")
			srv.cfg.Limiter = ratelimit.New(nil, ratelimit.WithSettings(ratelimit.Settings{TenantMaxInflight: 1}))
			srv.upstreams = upstreamclient.New(upstreamclient.Options{Client: &http.Client{
				Transport: responseTransport(func(*http.Request) (*http.Response, error) {
					if kind == "upstream" {
						return nil, errors.New("upstream failed")
					}
					var reader io.Reader = strings.NewReader("{}")
					if kind == "body" {
						reader = brokenResponseReader{}
					}
					header := make(http.Header)
					header.Set("Content-Type", "text/event-stream")
					return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(reader)}, nil
				}),
			}})
			receipts.failNext = strings.Contains(kind, "receipt")
			req := streamRequest(t.Context(), "http://gateway", "text/event-stream")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != 500 && w.Code != 502 {
				t.Fatalf("status = %d", w.Code)
			}
			key := srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"]
			release, delay := srv.cfg.Limiter.Acquire(tenantIdentity(key, "user-1"))
			if delay != 0 {
				t.Fatal("failure leaked a concurrency slot")
			}
			release()
		})
	}
}

func TestAct_SSEDisconnectReleasesTenantSlot(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer up.Close()
	srv, _ := bodyGateway(t, up.URL, "{}", true)
	srv.cfg.Limiter = ratelimit.New(nil, ratelimit.WithSettings(ratelimit.Settings{TenantMaxInflight: 1}))
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		srv.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	resp, err := server.Client().Do(streamRequest(ctx, server.URL, "text/event-stream"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	key := srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"]
	if _, delay := srv.cfg.Limiter.Acquire(tenantIdentity(key, "user-1")); delay == 0 {
		t.Fatal("stream should hold the tenant slot")
	}
	cancel()
	awaitSignal(t, done)
	release, delay := srv.cfg.Limiter.Acquire(tenantIdentity(key, "user-1"))
	if delay != 0 {
		t.Fatal("disconnect leaked the tenant slot")
	}
	release()
}
