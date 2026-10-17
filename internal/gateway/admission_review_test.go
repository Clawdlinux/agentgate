package gateway

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	upstreamclient "github.com/Clawdlinux/agentgate/internal/upstream"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func directAct(srv *Server, key, principal, service string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(fmt.Sprintf(
		`{"service":%q,"action":"list_repos","on_behalf_of":%q}`, service, principal)))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func installSuccessTransport(srv *Server) {
	srv.upstreams = upstreamclient.New(upstreamclient.Options{Client: &http.Client{
		Transport: responseTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}),
	}})
}

func TestAct_OutOfScopeCannotDrainAnotherTenant(t *testing.T) {
	for _, dimension := range []string{"principal", "service"} {
		t.Run(dimension, func(t *testing.T) {
			srv, receipts := bodyGateway(t, "http://unused", "{}", false)
			now := time.Unix(1000, 0)
			srv.cfg.Limiter = ratelimit.New(nil, ratelimit.WithSettings(ratelimit.Settings{
				KeyRPS: 1000, KeyBurst: 1000, TenantRPS: 1, TenantBurst: 1,
			}), ratelimit.WithClock(func() time.Time { return now }))
			alice := &auth.AgentKey{ID: "alice-key", AllowedUsers: []string{"alice"}, AllowedServices: []string{"github"}}
			if dimension == "service" {
				alice.AllowedUsers, alice.AllowedServices = []string{"bob"}, []string{"stripe"}
			}
			srv.cfg.Authorizer = &fakeAuthorizer{keys: map[string]*auth.AgentKey{
				"alice-key": alice,
				"bob-key":   {ID: "bob-key", AllowedUsers: []string{"bob"}, AllowedServices: []string{"github"}},
			}}
			if err := srv.cfg.Vault.Put("bob", "github", vault.Token{AccessToken: "secret"}); err != nil {
				t.Fatal(err)
			}
			installSuccessTransport(srv)
			verifier := &countingDelegation{}
			srv.cfg.Delegation = verifier
			for i := 0; i < 100; i++ {
				w := limitedAct(srv, "alice-key", "bob", "github")
				if w.Code != 403 || receipts.last().Error != "scope_denied" || receipts.last().PolicyDecision != "deny" {
					t.Fatalf("attempt=%d status=%d receipt=%+v", i, w.Code, receipts.last())
				}
				if i%10 == 0 {
					w = directAct(srv, "bob-key", "bob", "github")
					if w.Code != 200 {
						t.Fatalf("legitimate request status=%d body=%s", w.Code, w.Body)
					}
					now = now.Add(time.Second)
				}
			}
			if verifier.calls.Load() != 0 {
				t.Fatal("out-of-scope calls reached delegation verification")
			}
			if receipts.count() != 110 {
				t.Fatalf("receipt count = %d", receipts.count())
			}
		})
	}
}

func TestTenantIdentityBound(t *testing.T) {
	key := &auth.AgentKey{ID: "key-1"}
	for i := 0; i < 200; i++ {
		key.AllowedUsers = append(key.AllowedUsers, fmt.Sprint(i))
	}
	identities := make(map[string]bool)
	for _, principal := range key.AllowedUsers {
		identities[tenantIdentity(key, principal)] = true
	}
	if len(identities) != 64 {
		t.Fatalf("one key selected %d tenant entries", len(identities))
	}
	if tenantIdentity(key, "190") != tenantIdentity(key, "199") {
		t.Fatal("principals beyond the cap must share the key fallback")
	}
	key.AllowedUsers = []string{"*"}
	identities = make(map[string]bool)
	for i := 0; i < 200; i++ {
		identities[tenantIdentity(key, fmt.Sprint(i))] = true
	}
	if len(identities) != 1 {
		t.Fatalf("wildcard key selected %d tenant entries", len(identities))
	}
	other := &auth.AgentKey{ID: "key-2", AllowedUsers: []string{"*"}}
	if tenantIdentity(key, "alice") == tenantIdentity(other, "alice") {
		t.Fatal("wildcard fallbacks must be key-scoped")
	}
	key.AllowedUsers, other.AllowedUsers = []string{"alice"}, []string{"alice"}
	if tenantIdentity(key, "alice") != tenantIdentity(other, "alice") {
		t.Fatal("explicitly scoped keys must share the principal tenant")
	}
	other.AllowedUsers = []string{"*"}
	key.AllowedUsers = []string{"key:key-2"}
	if tenantIdentity(key, "key:key-2") == tenantIdentity(other, "alice") {
		t.Fatal("principal names must not collide with fallback identities")
	}
}

type advancingLimiter struct {
	*ratelimit.Limiter
	advance func()
}

func (l advancingLimiter) Acquire(tenant string) (func(), time.Duration) {
	l.advance()
	return l.Limiter.Acquire(tenant)
}

func TestAct_ConcurrencyDenialsRefundAllBuckets(t *testing.T) {
	srv, receipts := bodyGateway(t, "http://unused", "{}", false)
	installSuccessTransport(srv)
	now := time.Unix(1000, 0)
	l := ratelimit.New(map[string]ratelimit.Config{"github": {RequestsPerSecond: 1, Burst: 1}},
		ratelimit.WithSettings(ratelimit.Settings{
			KeyRPS: 1, KeyBurst: 1, TenantRPS: 1, TenantBurst: 1, TenantMaxInflight: 1,
		}), ratelimit.WithClock(func() time.Time { return now }))
	srv.cfg.Limiter = advancingLimiter{l, func() { now = now.Add(10 * time.Millisecond) }}
	key := srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"]
	release, delay := l.Acquire(tenantIdentity(key, "user-1"))
	if delay != 0 {
		t.Fatal(delay)
	}
	defer release()
	for i := 0; i < 10; i++ {
		w := transportAct(t, srv)
		if w.Code != 429 || receipts.last().Error != "concurrency_limited" {
			t.Fatalf("retry=%d status=%d receipt=%+v", i, w.Code, receipts.last())
		}
	}
	release()
	if w := transportAct(t, srv); w.Code != 200 {
		t.Fatalf("concurrency retries drained tokens: status=%d body=%s", w.Code, w.Body)
	}
	if w := transportAct(t, srv); w.Code != 429 || receipts.last().Error != "rate_limited" {
		t.Fatalf("refunds bypassed quota: status=%d receipt=%+v", w.Code, receipts.last())
	}
}

func TestAct_PanickingDispatchReleasesSlot(t *testing.T) {
	srv, _ := bodyGateway(t, "http://unused", "{}", false)
	l := ratelimit.New(nil, ratelimit.WithSettings(ratelimit.Settings{TenantMaxInflight: 1}))
	srv.cfg.Limiter = l
	var calls atomic.Int32
	srv.upstreams = upstreamclient.New(upstreamclient.Options{Client: &http.Client{
		Transport: responseTransport(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			panic("dispatch failed")
		}),
	}})
	done := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		srv.ServeHTTP(w, r)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Start()
	defer server.Close()
	req := streamRequest(t.Context(), server.URL, "application/json")
	resp, err := server.Client().Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || calls.Load() != 1 {
		t.Fatalf("dispatch did not panic: calls=%d err=%v", calls.Load(), err)
	}
	awaitSignal(t, done)
	key := srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"]
	release, delay := l.Acquire(tenantIdentity(key, "user-1"))
	if delay != 0 {
		t.Fatal("panic leaked the tenant slot")
	}
	release()
}

func TestAct_TenantFallbackRateLimit(t *testing.T) {
	for _, wildcard := range []bool{false, true} {
		t.Run(fmt.Sprintf("wildcard=%t", wildcard), func(t *testing.T) {
			srv, _ := bodyGateway(t, "http://unused", "{}", false)
			installSuccessTransport(srv)
			key := srv.cfg.Authorizer.(*fakeAuthorizer).keys["test-key"]
			key.AllowedUsers = []string{"*"}
			if !wildcard {
				key.AllowedUsers = nil
				for i := 0; i < 100; i++ {
					key.AllowedUsers = append(key.AllowedUsers, fmt.Sprint(i))
				}
			}
			for _, principal := range []string{"98", "99"} {
				if err := srv.cfg.Vault.Put(principal, "github", vault.Token{AccessToken: "secret"}); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Unix(1000, 0)
			srv.cfg.Limiter = ratelimit.New(nil, ratelimit.WithSettings(ratelimit.Settings{
				KeyRPS: 100, KeyBurst: 100, TenantRPS: 1, TenantBurst: 1,
			}), ratelimit.WithClock(func() time.Time { return now }))
			if w := directAct(srv, "test-key", "98", "github"); w.Code != 200 {
				t.Fatalf("first principal status=%d body=%s", w.Code, w.Body)
			}
			if w := directAct(srv, "test-key", "99", "github"); w.Code != 429 {
				t.Fatalf("fallback must aggregate principals, status=%d body=%s", w.Code, w.Body)
			}
		})
	}
}
