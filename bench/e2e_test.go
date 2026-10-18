//go:build bench

package bench

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/gateway"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

const warmupRequests = 256

type stack struct {
	db       *sql.DB
	ledger   *receipt.Ledger
	client   *http.Client
	url      string
	key      string
	upstream []chan time.Duration
}

type stackOptions struct {
	timingSlots  int
	spinUpstream bool
}

type benchmarkLogWriter struct {
	b *testing.B
}

func (w benchmarkLogWriter) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte(`"level":"ERROR"`)) {
		w.b.Logf("gateway error: %s", data)
	}
	return len(data), nil
}

func newStack(b *testing.B, concurrency int, delay time.Duration) *stack {
	b.Helper()
	return newStackWithOptions(b, concurrency, delay, stackOptions{})
}

func newStackWithOptions(b *testing.B, concurrency int, delay time.Duration, options stackOptions) *stack {
	b.Helper()
	database, err := agentgatedb.Open(filepath.Join(b.TempDir(), "agentgate.db"))
	must(b, err)
	b.Cleanup(func() { database.Close() })
	must(b, agentgatedb.RunMigrations(database))

	masterKey := make([]byte, 32)
	_, err = rand.Read(masterKey)
	must(b, err)
	pepper, err := auth.DeriveKeyPepper(masterKey)
	must(b, err)
	keys, err := auth.NewKeyStore(database, pepper)
	must(b, err)
	_, plaintext, err := keys.Create(context.Background(), "e2e-bench", []string{"mock"}, []string{"bench-user"})
	must(b, err)
	store, err := vault.NewSQLiteStore(database, masterKey)
	must(b, err)
	cached, err := vault.NewCachedStore(store)
	must(b, err)
	token := make([]byte, 32)
	_, err = rand.Read(token)
	must(b, err)
	credential := fmt.Sprintf("%x", token)
	must(b, cached.Put("bench-user", "mock", vault.Token{AccessToken: credential, TokenType: "Bearer"}))

	signers, err := signer.NewStore(database, masterKey)
	must(b, err)
	b.Cleanup(signers.Close)
	_, signingKey, err := signers.LoadOrCreateActive(1)
	clear(signingKey)
	must(b, err)
	ledger := receipt.NewLedger(database, signers)
	b.Cleanup(ledger.Close)

	timings := make([]chan time.Duration, max(concurrency, options.timingSlots))
	for i := range timings {
		timings[i] = make(chan time.Duration, 1)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var params struct {
			Worker int `json:"worker"`
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+credential {
			http.Error(w, "unexpected upstream request", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params.Worker < 0 || params.Worker >= len(timings) {
			http.Error(w, "invalid worker", http.StatusBadRequest)
			return
		}
		// Closed-loop workers serialize requests. Open-loop requests each have their own slot.
		defer func() { timings[params.Worker] <- time.Since(start) }()
		if delay > 0 {
			if options.spinUpstream {
				until := time.Now().Add(delay)
				for time.Now().Before(until) {
				}
			} else {
				time.Sleep(delay)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	b.Cleanup(upstream.Close)

	reg := registry.New()
	must(b, reg.LoadBytes([]byte(fmt.Sprintf(`services:
  mock:
    base_url: %s
    auth:
      type: bearer
    rate_limit:
      requests_per_second: 1000000
      burst: 1000000
    actions:
      echo:
        method: POST
        path: /echo
        params:
          worker: int
`, upstream.URL))))
	limiter := ratelimit.New(reg.RateLimits(), ratelimit.WithSettings(ratelimit.Settings{
		KeyRPS: 1e6, KeyBurst: 1e6, TenantRPS: 1e6, TenantBurst: 1e6, TenantMaxInflight: 1024,
	}))
	handler := gateway.New(gateway.Config{
		Registry: reg, Vault: cached, Authorizer: keys, Receipts: ledger, Limiter: limiter,
		Logger: slog.New(slog.NewJSONHandler(benchmarkLogWriter{b}, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	b.Cleanup(handler.CloseIdleConnections)
	server := httptest.NewServer(handler)
	b.Cleanup(server.Close)
	transport := &http.Transport{
		MaxIdleConns: concurrency, MaxIdleConnsPerHost: concurrency, MaxConnsPerHost: concurrency,
	}
	b.Cleanup(transport.CloseIdleConnections)
	return &stack{
		db: database, ledger: ledger, client: &http.Client{Transport: transport, Timeout: 10 * time.Second},
		url: server.URL + "/v1/act", key: plaintext, upstream: timings,
	}
}

type sample struct {
	total       time.Duration
	upstream    time.Duration
	dispatchLag time.Duration
}

func (s *stack) act(ctx context.Context, worker int, body string) (sample, error) {
	return s.actAt(ctx, worker, body, time.Time{})
}

func (s *stack) actAt(ctx context.Context, worker int, body string, scheduled time.Time) (sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, strings.NewReader(body))
	if err != nil {
		return sample{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return sample{}, err
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	finished := time.Now()
	total := finished.Sub(start)
	if err != nil {
		return sample{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return sample{}, fmt.Errorf("act status %d: %s", resp.StatusCode, data)
	}
	var envelope struct {
		Status int `json:"status"`
		Body   struct {
			OK bool `json:"ok"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return sample{}, err
	}
	if envelope.Status != http.StatusOK || !envelope.Body.OK {
		return sample{}, fmt.Errorf("unexpected act response: %s", data)
	}
	select {
	case duration := <-s.upstream[worker]:
		if duration > total {
			return sample{}, fmt.Errorf("upstream time %s exceeds total %s", duration, total)
		}
		return timedSample(scheduled, start, finished, duration), nil
	case <-ctx.Done():
		return sample{}, ctx.Err()
	}
}

func timedSample(scheduled, started, finished time.Time, upstream time.Duration) sample {
	if scheduled.IsZero() {
		scheduled = started
	}
	return sample{
		total: finished.Sub(scheduled), upstream: upstream, dispatchLag: started.Sub(scheduled),
	}
}

func (s *stack) run(count, concurrency int, samples []sample) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := make(chan error, concurrency)
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	for worker := range min(count, concurrency) {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			body := fmt.Sprintf(`{"service":"mock","action":"echo","on_behalf_of":"bench-user","params":{"worker":%d}}`, worker)
			ready.Done()
			<-start
			for i := worker; i < count; i += concurrency {
				result, err := s.act(ctx, worker, body)
				if err != nil {
					errors <- err
					cancel()
					return
				}
				if samples != nil {
					samples[i] = result
				}
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errors)
	for err := range errors {
		return err
	}
	return nil
}

func BenchmarkHandleActE2E(b *testing.B) {
	if runtime.GOMAXPROCS(0) != 4 {
		b.Fatal("run with GOMAXPROCS=4 for the Phase 0 comparison")
	}
	for _, delay := range []time.Duration{0, 5 * time.Millisecond} {
		b.Run("delay="+delay.String(), func(b *testing.B) {
			for _, concurrency := range []int{1, 16, 64} {
				b.Run(fmt.Sprintf("c=%d", concurrency), func(b *testing.B) {
					benchmarkAct(b, concurrency, delay)
				})
			}
		})
	}
}

func benchmarkAct(b *testing.B, concurrency int, delay time.Duration) {
	s := newStack(b, concurrency, delay)
	must(b, s.run(warmupRequests, concurrency, nil))
	samples := make([]sample, b.N)
	b.ResetTimer()
	start := time.Now()
	err := s.run(b.N, concurrency, samples)
	elapsed := time.Since(start)
	b.StopTimer()
	must(b, err)
	s.checkReceipts(b, warmupRequests+b.N)
	reportSamples(b, samples, elapsed)
}

func (s *stack) checkReceipts(b *testing.B, expected int) {
	b.Helper()
	var count, invalid int
	must(b, s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(
		CASE WHEN status_code != 200 OR policy_decision != 'allow' OR error_code != ''
		OR length(signature) != 64 THEN 1 ELSE 0 END), 0) FROM receipts`).Scan(&count, &invalid))
	head, _, err := s.ledger.Head(context.Background())
	must(b, err)
	if count != expected || head != uint64(count) || invalid != 0 {
		b.Fatalf("receipt mismatch: count=%d head=%d invalid=%d expected=%d", count, head, invalid, expected)
	}
}

func reportSamples(b *testing.B, samples []sample, elapsed time.Duration) {
	b.Helper()
	overheads := make([]time.Duration, b.N)
	totals := make([]time.Duration, b.N)
	var upstreamTotal time.Duration
	for i, result := range samples {
		overheads[i] = result.total - result.upstream
		totals[i] = result.total
		upstreamTotal += result.upstream
	}
	slices.Sort(overheads)
	slices.Sort(totals)
	for _, p := range []float64{50, 90, 99, 99.9} {
		b.ReportMetric(float64(percentile(overheads, p))/float64(time.Microsecond), fmt.Sprintf("p%g-overhead-us", p))
	}
	b.ReportMetric(float64(percentile(totals, 99))/float64(time.Microsecond), "p99-total-us")
	b.ReportMetric(float64(upstreamTotal)/float64(b.N)/float64(time.Microsecond), "upstream-mean-us")
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "req/s")
}

// percentile uses the nearest rank. Subtract upstream time before sorting, never between independently sorted percentiles.
func percentile(sorted []time.Duration, p float64) time.Duration {
	return sorted[max(0, int(math.Ceil(p*float64(len(sorted))/100))-1)]
}

func must(b *testing.B, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}

func TestPercentile(t *testing.T) {
	samples := make([]time.Duration, 1000)
	for i := range samples {
		samples[i] = time.Duration(i + 1)
	}
	for _, p := range []float64{50, 90, 99, 99.9} {
		if got, want := percentile(samples, p), time.Duration(math.Round(p*10)); got != want {
			t.Errorf("p%g: got %d, want %d", p, got, want)
		}
	}
	if got := percentile([]time.Duration{42}, 99.9); got != 42 {
		t.Errorf("single sample: got %d, want 42", got)
	}
}
