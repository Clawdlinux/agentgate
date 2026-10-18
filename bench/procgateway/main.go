// Command procgateway wires the real gateway stack for separate-process benchmarks.
// It binds loopback only. Never deploy this benchmark fixture.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"syscall"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	agentdb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/gateway"
	"github.com/Clawdlinux/agentgate/internal/ratelimit"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

type timings struct {
	auth, receipt time.Duration
}
type timingKey struct{}
type authorizer struct{ *auth.KeyStore }

func (a authorizer) Validate(ctx context.Context, key string) (*auth.AgentKey, error) {
	start := time.Now()
	result, err := a.KeyStore.Validate(ctx, key)
	ctx.Value(timingKey{}).(*timings).auth = time.Since(start)
	return result, err
}

type recorder struct{ *receipt.Ledger }

func (r recorder) Append(ctx context.Context, draft receipt.Draft) (receipt.Receipt, error) {
	start := time.Now()
	region := trace.StartRegion(ctx, "receipt.append")
	result, err := r.Ledger.Append(ctx, draft)
	region.End()
	ctx.Value(timingKey{}).(*timings).receipt = time.Since(start)
	return result, err
}

type responseWriter struct {
	http.ResponseWriter
	timing *timings
	start  time.Time
}

func (w responseWriter) WriteHeader(status int) {
	w.Header().Set("X-Bench-Auth-Ns", strconv.FormatInt(w.timing.auth.Nanoseconds(), 10))
	w.Header().Set("X-Bench-Receipt-Ns", strconv.FormatInt(w.timing.receipt.Nanoseconds(), 10))
	w.Header().Set("X-Bench-Handler-Ns", strconv.FormatInt(time.Since(w.start).Nanoseconds(), 10))
	w.ResponseWriter.WriteHeader(status)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "loopback listen address")
	upstream := flag.String("upstream", "http://127.0.0.1:19090", "mock upstream URL")
	path := flag.String("db", ".cache/proc/agentgate.db", "fresh SQLite path")
	credentials := flag.String("credentials", ".cache/proc/key.json", "private client credential file")
	profile := flag.String("profile", "", "profile prefix; empty disables profiling")
	batchDelay := flag.Duration("batch-delay", 0, "receipt collection delay")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	must(err)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		log.Fatal("benchmark server requires a loopback address")
	}
	database, err := agentdb.Open(*path)
	must(err)
	defer database.Close()
	must(agentdb.RunMigrations(database))
	var journal string
	var synchronous, busyTimeout, foreignKeys, autoCheckpoint, fullFSync int
	must(database.QueryRow("PRAGMA journal_mode").Scan(&journal))
	must(database.QueryRow("PRAGMA synchronous").Scan(&synchronous))
	must(database.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout))
	must(database.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys))
	must(database.QueryRow("PRAGMA wal_autocheckpoint").Scan(&autoCheckpoint))
	must(database.QueryRow("PRAGMA fullfsync").Scan(&fullFSync))
	log.Printf("SQLite journal=%s synchronous=%d busy_timeout=%d foreign_keys=%d wal_autocheckpoint=%d fullfsync=%d GOMAXPROCS=%d",
		journal, synchronous, busyTimeout, foreignKeys, autoCheckpoint, fullFSync, runtime.GOMAXPROCS(0))

	master := make([]byte, 32)
	_, err = rand.Read(master)
	must(err)
	defer clear(master)
	pepper, err := auth.DeriveKeyPepper(master)
	must(err)
	keys, err := auth.NewKeyStore(database, pepper)
	must(err)
	_, key, err := keys.Create(context.Background(), "proc-bench", []string{"mock"}, []string{"bench-user"})
	must(err)
	store, err := vault.NewSQLiteStore(database, master)
	must(err)
	cache, err := vault.NewCachedStore(store)
	must(err)
	token := make([]byte, 32)
	_, err = rand.Read(token)
	must(err)
	must(cache.Put("bench-user", "mock", vault.Token{AccessToken: fmt.Sprintf("%x", token), TokenType: "Bearer"}))
	signers, err := signer.NewStore(database, master)
	must(err)
	defer signers.Close()
	_, private, err := signers.LoadOrCreateActive(1)
	clear(private)
	must(err)
	ledger := receipt.NewLedger(database, signers, receipt.WithBatchDelay(*batchDelay))
	defer ledger.Close()
	reg := registry.New()
	must(reg.LoadBytes([]byte(fmt.Sprintf(`services:
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
`, *upstream))))
	limiter := ratelimit.New(reg.RateLimits(), ratelimit.WithSettings(ratelimit.Settings{
		KeyRPS: 1e6, KeyBurst: 1e6, TenantRPS: 1e6, TenantBurst: 1e6, TenantMaxInflight: 1024,
	}))
	handler := gateway.New(gateway.Config{
		Registry: reg, Vault: cache, Authorizer: authorizer{keys}, Receipts: recorder{ledger}, Limiter: limiter,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	defer handler.CloseIdleConnections()
	encoded, err := json.Marshal(map[string]string{"key": key})
	must(err)
	must(os.WriteFile(*credentials, encoded, 0600))
	defer os.Remove(*credentials)

	if *profile != "" {
		cpu, err := os.Create(*profile + ".cpu")
		must(err)
		defer cpu.Close()
		must(pprof.StartCPUProfile(cpu))
		runtime.SetMutexProfileFraction(1)
		runtime.SetBlockProfileRate(1)
		traceFile, err := os.Create(*profile + ".trace")
		must(err)
		defer traceFile.Close()
		must(trace.Start(traceFile))
		defer func() {
			trace.Stop()
			pprof.StopCPUProfile()
			for _, name := range []string{"mutex", "block", "allocs", "heap"} {
				file, err := os.Create(*profile + "." + name)
				must(err)
				must(pprof.Lookup(name).WriteTo(file, 0))
				must(file.Close())
			}
		}()
	}

	server := &http.Server{Addr: *addr, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bench/head" {
			head, _, err := ledger.Head(r.Context())
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			json.NewEncoder(w).Encode(map[string]uint64{
				"head": head, "total_alloc": stats.TotalAlloc, "num_gc": uint64(stats.NumGC),
				"pause_total_ns": stats.PauseTotalNs,
			})
			return
		}
		timing := &timings{}
		ctx, task := trace.NewTask(context.WithValue(r.Context(), timingKey{}, timing), "act")
		defer task.End()
		trace.Log(ctx, "request.id", r.Header.Get("X-Bench-ID"))
		if *profile != "" {
			traceContext := ctx
			ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
				GetConn: func(string) { trace.Log(traceContext, "upstream.getconn", "") },
				GotConn: func(info httptrace.GotConnInfo) {
					trace.Log(traceContext, "upstream.gotconn", strconv.FormatBool(info.Reused))
				},
			})
		}
		handler.ServeHTTP(responseWriter{w, timing, time.Now()}, r.WithContext(ctx))
	})}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	must(server.Shutdown(shutdown))
}
