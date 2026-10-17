package upstream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1700000000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func retries(n int) RetryConfig { return RetryConfig{MaxRetries: &n} }

func testPool(t *testing.T, options Options) *Pool {
	t.Helper()
	p := New(options)
	t.Cleanup(p.CloseIdleConnections)
	return p
}

func request(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func readResponse(t *testing.T, resp *http.Response, err error) int {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestPerServiceTransports(t *testing.T) {
	p := testPool(t, Options{})
	a := p.client("a", Config{})
	b := p.client("b", Config{
		DialTimeout: 2 * time.Second, KeepAlive: 20 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout: time.Minute, MaxIdleConns: 128,
		MaxIdleConnsPerHost: 64, MaxConnsPerHost: 100, RequestTimeout: time.Minute,
	})
	if a == b || a.transport == b.transport || a.client == b.client {
		t.Fatal("services share transport state")
	}
	if p.client("a", Config{}) != a {
		t.Fatal("service client was not reused")
	}
	for _, tc := range []struct {
		state                     *clientState
		tls, headers, idle        time.Duration
		maxIdle, perHost, maxHost int
	}{
		{a, 5 * time.Second, 30 * time.Second, 90 * time.Second, 256, 32, 0},
		{b, 3 * time.Second, 10 * time.Second, time.Minute, 128, 64, 100},
	} {
		tr := tc.state.transport
		if tr.DialContext == nil || tr.Proxy == nil || !tr.ForceAttemptHTTP2 {
			t.Fatal("missing dialer, proxy, or HTTP/2")
		}
		if tr.TLSHandshakeTimeout != tc.tls || tr.ResponseHeaderTimeout != tc.headers ||
			tr.IdleConnTimeout != tc.idle || tr.MaxIdleConns != tc.maxIdle ||
			tr.MaxIdleConnsPerHost != tc.perHost || tr.MaxConnsPerHost != tc.maxHost {
			t.Fatalf("incorrect transport: %+v", tr)
		}
		if tc.state.client.Timeout != 0 {
			t.Fatal("deadline must come from request context")
		}
	}
	if a.config.DialTimeout != 5*time.Second || a.config.KeepAlive != 30*time.Second ||
		b.config.DialTimeout != 2*time.Second || b.config.KeepAlive != 20*time.Second {
		t.Fatal("dial settings were not applied")
	}
}

func TestResponseHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	p := testPool(t, Options{})
	start := time.Now()
	resp, err := p.Do("slow", Config{ResponseHeaderTimeout: 20 * time.Millisecond, Retry: retries(0)},
		request(t, "GET", server.URL, nil), false)
	if resp != nil || err == nil {
		t.Fatalf("response=%v error=%v", resp, err)
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected header timeout, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("header timeout did not fire promptly")
	}
}

func TestRetryMethodsAndReplay(t *testing.T) {
	for _, tc := range []struct {
		method     string
		idempotent bool
		calls      int32
	}{
		{"GET", false, 2}, {"HEAD", false, 2}, {"OPTIONS", false, 2},
		{"POST", false, 1}, {"POST", true, 1}, {"PATCH", true, 1},
		{"PUT", false, 1}, {"PUT", true, 2},
		{"DELETE", false, 1}, {"DELETE", true, 2},
	} {
		t.Run(tc.method+":"+stringBool(tc.idempotent), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if string(body) != "payload" {
					t.Errorf("replayed body = %q", body)
				}
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					return
				}
				w.WriteHeader(200)
			}))
			defer server.Close()
			p := testPool(t, Options{Clock: newFakeClock()})
			resp, err := p.Do("service", Config{}, request(t, tc.method, server.URL, strings.NewReader("payload")), tc.idempotent)
			status := readResponse(t, resp, err)
			if calls.Load() != tc.calls || (tc.calls == 2 && status != 200) {
				t.Fatalf("calls=%d status=%d", calls.Load(), status)
			}
		})
	}
}

func stringBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestRetryStatusAndLimit(t *testing.T) {
	for _, status := range []int{400, 429, 500, 501, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()
			p := testPool(t, Options{Clock: newFakeClock()})
			resp, err := p.Do("service", Config{}, request(t, "GET", server.URL, nil), false)
			if got := readResponse(t, resp, err); got != status {
				t.Fatalf("status=%d", got)
			}
			want := int32(1)
			if status == 502 || status == 503 || status == 504 {
				want = 3
			}
			if calls.Load() != want {
				t.Fatalf("calls=%d want=%d", calls.Load(), want)
			}
		})
	}
}

func TestRetryAfterAndBackoff(t *testing.T) {
	for _, value := range []string{"1", "100", "99999999999999999999999999999999", "Wed, 15 Nov 2023 00:13:21 GMT", "invalid"} {
		t.Run(value, func(t *testing.T) {
			clock := newFakeClock()
			// Use the injected clock's exact HTTP date.
			header := value
			if strings.Contains(value, "GMT") {
				header = clock.Now().Add(time.Second).UTC().Format(http.TimeFormat)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) < 3 {
					w.Header().Set("Retry-After", header)
					w.WriteHeader(503)
				}
			}))
			defer server.Close()
			p := testPool(t, Options{Clock: clock, Jitter: func(limit time.Duration) time.Duration { return limit / 2 }})
			resp, err := p.Do("service", Config{}, request(t, "GET", server.URL, nil), false)
			readResponse(t, resp, err)
			if len(clock.sleeps) != 2 {
				t.Fatalf("sleeps=%v", clock.sleeps)
			}
			want := 50 * time.Millisecond
			switch value {
			case "1":
				want = time.Second
			case "100", "99999999999999999999999999999999":
				want = 2 * time.Second
			default:
				if strings.Contains(value, "GMT") {
					want = time.Second
				}
			}
			if clock.sleeps[0] != want {
				t.Fatalf("delay=%v want=%v", clock.sleeps[0], want)
			}
			if value == "invalid" && clock.sleeps[1] != 100*time.Millisecond {
				t.Fatalf("exponential backoff=%v", clock.sleeps)
			}
		})
	}
}

func TestBreakerProbeAndIsolation(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !healthy.Load() {
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	p := testPool(t, Options{Clock: clock})
	cfg := Config{Breaker: BreakerConfig{FailureThreshold: 2, Cooldown: time.Second}, Retry: retries(0)}
	for i := 0; i < 2; i++ {
		resp, err := p.Do("a", cfg, request(t, "GET", server.URL, nil), false)
		readResponse(t, resp, err)
	}
	for i := 0; i < 10; i++ {
		_, err := p.Do("a", cfg, request(t, "GET", server.URL, nil), false)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("open breaker error=%v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("open breaker sent traffic")
	}
	resp, err := p.Do("b", cfg, request(t, "GET", server.URL, nil), false)
	readResponse(t, resp, err)
	clock.advance(time.Second)
	healthy.Store(true)
	for i := 0; i < 2; i++ {
		resp, err := p.Do("a", cfg, request(t, "GET", server.URL, nil), false)
		if readResponse(t, resp, err) != 200 {
			t.Fatal("probe did not close breaker")
		}
	}
}

func TestBreakerSingleProbeAndStaleResults(t *testing.T) {
	clock := newFakeClock()
	b := newBreaker(BreakerConfig{FailureThreshold: 1, Cooldown: time.Second}, clock)
	old, ok := b.acquire()
	if !ok {
		t.Fatal("closed breaker rejected request")
	}
	fail, _ := b.acquire()
	b.complete(fail, true)
	b.complete(old, false)
	if _, ok := b.acquire(); ok {
		t.Fatal("stale success closed open breaker")
	}
	clock.advance(time.Second)
	probe, ok := b.acquire()
	if !ok {
		t.Fatal("probe rejected")
	}
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := b.acquire(); ok {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 0 {
		t.Fatalf("extra probes=%d", admitted.Load())
	}
	b.complete(probe, true)
	if _, ok := b.acquire(); ok {
		t.Fatal("failed probe did not reopen breaker")
	}
	clock.advance(time.Second)
	probe, ok = b.acquire()
	if !ok {
		t.Fatal("second probe rejected")
	}
	b.complete(probe, false)
	if _, ok := b.acquire(); !ok {
		t.Fatal("successful probe did not close breaker")
	}
}

func TestGlobalRetryBudget(t *testing.T) {
	clock := newFakeClock()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
	}))
	defer server.Close()
	p := testPool(t, Options{Clock: clock, Jitter: func(time.Duration) time.Duration { return 0 }})
	cfg := Config{Breaker: BreakerConfig{FailureThreshold: 1000}, Retry: retries(1)}
	for i := 0; i < 20; i++ {
		resp, err := p.Do(string(rune('a'+i)), cfg, request(t, "GET", server.URL, nil), false)
		readResponse(t, resp, err)
	}
	if calls.Load() != 30 {
		t.Fatalf("calls=%d, want 20 requests plus 10 retries", calls.Load())
	}
	clock.advance(time.Second)
	resp, err := p.Do("new", cfg, request(t, "GET", server.URL, nil), false)
	readResponse(t, resp, err)
	if calls.Load() != 32 {
		t.Fatal("budget did not refill")
	}
}

func TestContextStopsRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(503)
	}))
	defer server.Close()
	p := testPool(t, Options{})
	for _, parentDeadline := range []bool{false, true} {
		ctx := t.Context()
		if parentDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
			defer cancel()
		}
		req := request(t, "GET", server.URL, nil).WithContext(ctx)
		start := time.Now()
		_, err := p.Do("service", Config{RequestTimeout: 30 * time.Millisecond}, req, false)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v", err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("deadline did not stop retry sleep")
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d, want no retries", calls.Load())
	}
}

func TestDeadlineCoversResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	p := testPool(t, Options{})
	resp, err := p.Do("service", Config{RequestTimeout: 20 * time.Millisecond},
		request(t, "GET", server.URL, nil), false)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body error=%v", err)
	}
}

func TestConnectionReuse(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	p := testPool(t, Options{})
	for i := 0; i < 50; i++ {
		resp, err := p.Do("service", Config{}, request(t, "GET", server.URL, nil), false)
		readResponse(t, resp, err)
	}
	if connections.Load() != 1 {
		t.Fatalf("connections=%d, want 1", connections.Load())
	}
}

func TestNoRetryWithoutReplayableBody(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.WriteHeader(503)
	}))
	defer server.Close()
	p := testPool(t, Options{Clock: newFakeClock()})
	body := io.NopCloser(strings.NewReader("not replayable"))
	resp, err := p.Do("service", Config{}, request(t, "PUT", server.URL, body), true)
	readResponse(t, resp, err)
	if calls.Load() != 1 {
		t.Fatal("non-replayable body retried")
	}
}
