package upstream

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRedirectCannotCrossOrigin(t *testing.T) {
	for _, customPolicy := range []bool{false, true} {
		t.Run(stringBool(customPolicy), func(t *testing.T) {
			var calls atomic.Int32
			var sourceCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls.Add(1)
				http.Redirect(w, r, target.URL, http.StatusFound)
			}))
			defer source.Close()
			var custom *http.Client
			if customPolicy {
				custom = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
			}
			p := testPool(t, Options{Client: custom})
			req := request(t, "GET", source.URL, nil)
			req.Header.Set("X-API-Key", "private-token")
			cfg := Config{Breaker: BreakerConfig{FailureThreshold: 1}}
			resp, err := p.Do("service", cfg, req, false)
			if resp != nil {
				resp.Body.Close()
			}
			if calls.Load() != 0 || err == nil {
				t.Fatalf("cross-origin redirect followed: calls=%d error=%v", calls.Load(), err)
			}
			if sourceCalls.Load() != 1 {
				t.Fatalf("redirect rejection retried: calls=%d", sourceCalls.Load())
			}
			b := p.client("service", cfg).breaker
			b.mu.Lock()
			state, failures := b.state, b.failures
			b.mu.Unlock()
			if state != closed || failures != 0 {
				t.Fatalf("local redirect policy changed breaker: state=%d failures=%d", state, failures)
			}
		})
	}
}

func TestRedirectRejectsSchemeChanges(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		allowed  bool
	}{
		{"https://api.example.com/start", "http://api.example.com/target", false},
		{"http://api.example.com/start", "https://api.example.com/target", false},
		{"https://api.example.com/start", "https://api.example.com:8443/target", false},
		{"https://api.example.com/start", "https://API.example.com/target", true},
	} {
		t.Run(tc.from+":"+tc.to, func(t *testing.T) {
			p := testPool(t, Options{})
			policy := p.client("service", Config{}).client.CheckRedirect
			if policy == nil {
				t.Fatal("missing redirect policy")
			}
			err := policy(request(t, "GET", tc.to, nil), []*http.Request{request(t, "GET", tc.from, nil)})
			if (err == nil) != tc.allowed {
				t.Fatalf("redirect error=%v allowed=%v", err, tc.allowed)
			}
		})
	}
}

func TestRedirectPreservesStricterCustomPolicy(t *testing.T) {
	p := testPool(t, Options{Client: &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}})
	policy := p.client("service", Config{}).client.CheckRedirect
	err := policy(request(t, "GET", "https://example.com/to", nil),
		[]*http.Request{request(t, "GET", "https://example.com/from", nil)})
	if err != http.ErrUseLastResponse {
		t.Fatalf("custom policy error=%v", err)
	}
}

func TestSameOriginRedirectHopLimit(t *testing.T) {
	for _, hops := range []int{5, 6} {
		t.Run(strconv.Itoa(hops), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("X-API-Key") != "private-token" {
					t.Error("same-origin credential missing")
				}
				step, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
				if step < hops {
					http.Redirect(w, r, fmt.Sprintf("/%d", step+1), http.StatusFound)
				}
			}))
			defer server.Close()
			p := testPool(t, Options{})
			req := request(t, "GET", server.URL+"/0", nil)
			req.Header.Set("X-API-Key", "private-token")
			resp, err := p.Do("service", Config{Retry: retries(0)}, req, false)
			if hops == 5 {
				if readResponse(t, resp, err) != 200 || calls.Load() != 6 {
					t.Fatalf("calls=%d", calls.Load())
				}
			} else {
				if resp != nil {
					resp.Body.Close()
				}
				if err == nil || calls.Load() != 6 {
					t.Fatalf("error=%v calls=%d", err, calls.Load())
				}
			}
		})
	}
}
