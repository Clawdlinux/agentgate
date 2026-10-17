package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallerCancellationDoesNotOpenBreaker(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(stringBool(deadline), func(t *testing.T) {
			started := make(chan struct{}, 5)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/healthy" {
					w.WriteHeader(200)
					return
				}
				started <- struct{}{}
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				want = context.DeadlineExceeded
			}
			defer cancel()
			p := testPool(t, Options{})
			cfg := Config{Retry: retries(0)}
			results := make(chan error, 5)
			for i := 0; i < 5; i++ {
				req := request(t, "GET", server.URL+"/cancel", nil).WithContext(ctx)
				go func() {
					resp, err := p.Do("service", cfg, req, false)
					if resp != nil {
						resp.Body.Close()
					}
					results <- err
				}()
			}
			for i := 0; i < 5; i++ {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("caller stopped before request reached upstream")
				case <-time.After(5 * time.Second):
					t.Fatal("request did not reach upstream")
				}
			}
			if !deadline {
				cancel()
			}
			for i := 0; i < 5; i++ {
				select {
				case err := <-results:
					if !errors.Is(err, want) {
						t.Fatalf("error=%v want=%v", err, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("canceled request did not return")
				}
			}
			b := p.client("service", cfg).breaker
			b.mu.Lock()
			state, failures := b.state, b.failures
			b.mu.Unlock()
			if state != closed || failures != 0 {
				t.Fatalf("caller cancellation changed breaker: state=%d failures=%d", state, failures)
			}
			resp, err := p.Do("service", cfg, request(t, "GET", server.URL+"/healthy", nil), false)
			if readResponse(t, resp, err) != 200 {
				t.Fatal("healthy caller was blocked")
			}
		})
	}
}

func TestCanceledHalfOpenProbeReleasesPermit(t *testing.T) {
	clock := newFakeClock()
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/failure":
			w.WriteHeader(500)
		case "/probe":
			started <- struct{}{}
			<-r.Context().Done()
		default:
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	p := testPool(t, Options{Clock: clock})
	cfg := Config{Retry: retries(0), Breaker: BreakerConfig{FailureThreshold: 1, Cooldown: 30 * time.Second}}
	resp, err := p.Do("service", cfg, request(t, "GET", server.URL+"/failure", nil), false)
	if readResponse(t, resp, err) != 500 {
		t.Fatal("failure not returned")
	}
	clock.advance(30 * time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, 1)
	req := request(t, "GET", server.URL+"/probe", nil).WithContext(ctx)
	go func() {
		resp, err := p.Do("service", cfg, req, false)
		if resp != nil {
			resp.Body.Close()
		}
		results <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not reach upstream")
	}
	clock.advance(5 * time.Second)
	cancel()
	select {
	case err := <-results:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("probe error=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe cancellation did not return")
	}
	resp, err = p.Do("service", cfg, request(t, "GET", server.URL+"/healthy", nil), false)
	if readResponse(t, resp, err) != 200 || calls.Load() != 3 {
		t.Fatalf("canceled probe restarted cooldown: calls=%d", calls.Load())
	}
}

func TestBreakerReleasePreservesFailuresAndRejectsStalePermits(t *testing.T) {
	clock := newFakeClock()
	b := newBreaker(BreakerConfig{FailureThreshold: 2, Cooldown: time.Second}, clock)
	permit, _ := b.acquire()
	b.complete(permit, true)
	permit, _ = b.acquire()
	b.release(permit)
	if b.state != closed || b.failures != 1 {
		t.Fatalf("release reset closed breaker: state=%d failures=%d", b.state, b.failures)
	}
	permit, _ = b.acquire()
	b.complete(permit, true)
	clock.advance(time.Second)
	oldProbe, ok := b.acquire()
	if !ok {
		t.Fatal("probe not admitted")
	}
	b.release(oldProbe)
	newProbe, ok := b.acquire()
	if !ok {
		t.Fatal("released probe restarted cooldown")
	}
	b.release(oldProbe)
	b.complete(oldProbe, false)
	if _, ok := b.acquire(); ok {
		t.Fatal("stale permit released the current probe")
	}
	b.complete(newProbe, false)
	if _, ok := b.acquire(); !ok {
		t.Fatal("current probe did not close breaker")
	}
}

func TestPoolTimeoutStillOpensBreaker(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	p := testPool(t, Options{})
	cfg := Config{RequestTimeout: 30 * time.Millisecond, Retry: retries(0)}
	for i := 0; i < 5; i++ {
		req := request(t, "GET", server.URL, nil)
		_, err := p.Do("service", cfg, req, false)
		if !errors.Is(err, context.DeadlineExceeded) || req.Context().Err() != nil {
			t.Fatalf("error=%v caller=%v", err, req.Context().Err())
		}
	}
	_, err := p.Do("service", cfg, request(t, "GET", server.URL, nil), false)
	if !errors.Is(err, ErrUnavailable) || calls.Load() != 5 {
		t.Fatalf("pool timeouts did not open breaker: error=%v calls=%d", err, calls.Load())
	}
}
