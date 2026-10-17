package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedBody) Close() error { b.closed.Store(true); return nil }

func TestRetryClosesDiscardedBodies(t *testing.T) {
	var previous *trackedBody
	var calls int
	var contexts []context.Context
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if previous != nil && !previous.closed.Load() {
			t.Fatal("previous retry body was not closed")
		}
		calls++
		contexts = append(contexts, r.Context())
		previous = &trackedBody{Reader: strings.NewReader("upstream unavailable")}
		status := 503
		if calls == 3 {
			status = 200
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: previous}, nil
	})
	p := testPool(t, Options{Clock: newFakeClock(), Client: &http.Client{Transport: transport}})
	resp, err := p.Do("service", Config{}, request(t, "GET", "http://unused", nil), false)
	if err != nil {
		t.Fatal(err)
	}
	if previous.closed.Load() {
		t.Fatal("final body closed before caller read it")
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
	readResponse(t, resp, nil)
	if !previous.closed.Load() {
		t.Fatal("final body not closed")
	}
	for _, ctx := range contexts {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("request context leaked: %v", ctx.Err())
		}
	}
}

func TestRetryTransportFailure(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				io.WriteString(w, "ok")
			}))
			defer server.Close()
			p := testPool(t, Options{Clock: newFakeClock()})
			resp, err := p.Do("service", Config{}, request(t, method, server.URL, nil), false)
			if method == "POST" {
				if err == nil || calls.Load() != 1 {
					t.Fatalf("calls=%d err=%v", calls.Load(), err)
				}
				return
			}
			if readResponse(t, resp, err) != 200 || calls.Load() != 2 {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
}

func TestBreakerFourHundredsResetFailures(t *testing.T) {
	clock := newFakeClock()
	b := newBreaker(BreakerConfig{FailureThreshold: 2, Cooldown: time.Second}, clock)
	for i := 0; i < 10; i++ {
		permit, ok := b.acquire()
		if !ok {
			t.Fatal("breaker opened on a non-failure")
		}
		b.complete(permit, i%2 == 0)
	}
}

func TestBudgetSlidingWindowAndConcurrency(t *testing.T) {
	clock := newFakeClock()
	b := newRetryBudget(clock)
	for i := 0; i < 1000; i++ {
		b.recordRequest()
	}
	var allowed atomic.Int32
	consume := func() {
		var wg sync.WaitGroup
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if b.allow() {
					allowed.Add(1)
				}
			}()
		}
		wg.Wait()
	}
	consume()
	if allowed.Load() != 10 {
		t.Fatalf("initial tokens=%d", allowed.Load())
	}
	clock.advance(500 * time.Millisecond)
	consume()
	if allowed.Load() != 110 {
		t.Fatalf("rate did not track 20%% of requests: %d", allowed.Load())
	}
	clock.advance(2 * time.Second)
	allowed.Store(0)
	consume()
	if allowed.Load() != 10 {
		t.Fatalf("expired requests still affect budget: %d", allowed.Load())
	}
	clock.advance(time.Second)
	allowed.Store(0)
	consume()
	if allowed.Load() != 10 {
		t.Fatalf("floor did not refill at 10/s: %d", allowed.Load())
	}
}

func TestBudgetLimitsSlidingWindowBursts(t *testing.T) {
	clock := newFakeClock()
	b := newRetryBudget(clock)
	for i := 0; i < 100; i++ {
		b.recordRequest()
	}
	allowed := 0
	for step := 0; step < 10; step++ {
		if step != 0 {
			clock.advance(99 * time.Millisecond)
		}
		for b.allow() {
			allowed++
		}
	}
	if allowed > 20 {
		t.Fatalf("retries=%d exceed 20%% of 100 requests", allowed)
	}
	clock.advance(2 * time.Second)
	for b.allow() {
	}
	clock.advance(100 * time.Millisecond)
	if b.allow() {
		t.Fatal("sliding floor exceeded 10 retries/s")
	}
}

func TestExhaustedBudgetPreservesFinalBody(t *testing.T) {
	clock := newFakeClock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		io.WriteString(w, "final error response")
	}))
	defer server.Close()
	p := testPool(t, Options{Clock: clock})
	for i := 0; i < 10; i++ {
		if !p.budget.allow() {
			t.Fatal("initial budget empty")
		}
	}
	resp, err := p.Do("service", Config{}, request(t, "GET", server.URL, nil), false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "final error response" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if len(clock.sleeps) != 0 {
		t.Fatal("retry slept without budget")
	}
}

func TestDefaultBreakerThreshold(t *testing.T) {
	p := testPool(t, Options{Clock: newFakeClock()})
	b := p.client("service", Config{}).breaker
	for i := 0; i < 5; i++ {
		permit, ok := b.acquire()
		if !ok {
			t.Fatalf("opened before failure %d", i+1)
		}
		b.complete(permit, true)
	}
	if _, ok := b.acquire(); ok {
		t.Fatal("breaker did not open after 5 failures")
	}
}

func TestRetryBackoffCap(t *testing.T) {
	var limits []time.Duration
	p := testPool(t, Options{Clock: newFakeClock(), Jitter: func(limit time.Duration) time.Duration {
		limits = append(limits, limit)
		return limit / 2
	}})
	cfg := Config{}.defaults().Retry
	for i := 0; i < 100; i++ {
		delay := p.retryDelay(cfg, i, nil)
		if delay < 0 || delay > 2*time.Second {
			t.Fatalf("backoff=%v", delay)
		}
	}
	if limits[0] != 100*time.Millisecond || limits[1] != 200*time.Millisecond || limits[99] != 2*time.Second {
		t.Fatalf("limits=%v", limits)
	}
}

func TestCustomClientKeepsTLSButUsesContextDeadline(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("protocol=%s, want HTTP/2", r.Proto)
		}
		io.WriteString(w, "ok")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	custom := server.Client()
	custom.Timeout = time.Nanosecond
	p := testPool(t, Options{Client: custom})
	for _, name := range []string{"a", "b"} {
		resp, err := p.Do(name, Config{}, request(t, "GET", server.URL, nil), false)
		readResponse(t, resp, err)
		if p.clients[name].client.Timeout != 0 {
			t.Fatal("custom client timeout survived")
		}
	}
	if p.clients["a"].transport == p.clients["b"].transport || p.clients["a"].transport == custom.Transport {
		t.Fatal("custom client transport was shared")
	}
}

func TestFastFailureClosesRequestBody(t *testing.T) {
	p := testPool(t, Options{Clock: newFakeClock()})
	b := p.client("service", Config{Breaker: BreakerConfig{FailureThreshold: 1}}).breaker
	permit, _ := b.acquire()
	b.complete(permit, true)
	body := &trackedBody{Reader: strings.NewReader("payload")}
	_, err := p.Do("service", Config{}, request(t, "PUT", "http://unused", body), false)
	if !errors.Is(err, ErrUnavailable) || !body.closed.Load() {
		t.Fatalf("error=%v body closed=%v", err, body.closed.Load())
	}
}
