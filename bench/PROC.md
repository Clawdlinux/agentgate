# Separate-process latency benchmark

Run `make bench-proc` from the repository root. No Docker or extra Go dependencies are needed.
Python 3 supervises 3 compiled Go processes:

- `bench/mockupstream` listens on loopback port `19090`, with `GOMAXPROCS=2`.
- `bench/procgateway` listens on loopback port `18080`, with `GOMAXPROCS=4`.
- `bench/loadgen` sends requests with its own `GOMAXPROCS=2`.

These processes share host CPUs. `GOMAXPROCS` isn't CPU pinning or a 4-vCPU quota.
Default process priority remains unchanged. No privileged priority change is needed.
Stop unrelated CPU and disk workloads before measuring.
Run on a dedicated Linux 4-vCPU gateway host before making deployment claims.

## Stack and timing

The thin gateway main wires the real gateway, authentication, encrypted vault, and signing stores.
It uses real SQLite files and migrations, plus the default receipt ledger and upstream pool.
All rate checks remain active, with high benchmark limits.
Requests use a scoped HMAC key and an unexpired cached bearer token.
No delegation token is presented. Production rejects invalid tokens through its unchanged verifier path.
Info logs still undergo JSON encoding, but writes go to `io.Discard`.
Terminal and remote log-sink delays aren't measured.

Each case gets a fresh database and 256 warmup requests.
The default matrix measures 20,000 requests per case.
It includes fixed arrivals at 1,000, 2,500, 5,000, and 10,000 requests/s.
It also includes closed-loop concurrency 1 and 16.
Both modes run with 0 ms and 5 ms mock delays.

Open-loop latency starts at each request's original scheduled arrival.
Late arrivals never reset the schedule or drop requests.
Dispatch lag and client connection waits stay in latency.
The client uses at most 1,024 connections. Pool waits remain measured.
Closed-loop timing starts immediately before `Client.Do`.
Both timers end after reading and closing the entire response body.

The mock reports its measured handler duration, including sleep overshoot, inside `body.upstream_ns`.
Response encoding and writing stay in overhead.

```text
overhead = response_completed - scheduled_arrival - upstream_ns
```

Subtraction occurs per request, before nearest-rank percentiles.
All requests must succeed. The ledger head must advance by exactly the measured count.
The client records request samples, connection reuse, and gateway timing headers.
Those headers exist only in this loopback benchmark main, not the public gateway.
`handler_ns` ends before response encoding. `receipt_ns` includes ledger queueing and committed acknowledgment.
Receipt time and auth time are nested inside handler time. Don't add overlapping spans.
These aren't isolated handler CPU costs. Both loopback paths and scheduler delays remain included.

## Reproduction

```sh
make bench-proc
make bench-proc BENCH_PROC_ARGS='--label repeat'
make bench-proc BENCH_PROC_ARGS='--label smoke --n 64'
make bench-proc BENCH_PROC_ARGS='--label profile --profile --rates 5000 --closed "" --delays 0'
```

Profiles change timing. Don't use profiled requests for the latency gate.
Each run prints its unique directory under ignored `.cache/proc/`.
Every case stores its CSV samples, summary JSON, private database, and gateway startup pragmas.
Credential files use mode `0600` and disappear when the gateway stops.
The supervisor stops all children, including after failures.
Keep databases and profiles private. Don't commit `.cache/`.

Profiling records CPU, allocation, block, mutex, and execution trace files.
It also enables `GODEBUG=gctrace=1` in the gateway process.
Receipt trace regions distinguish head reads, signing, inserts, and SQLite COMMIT.
Request task logs identify individual load-generator sample indices.

```sh
go tool pprof -top .cache/proc/bin/procgateway PATH/gateway.cpu
go tool pprof -top .cache/proc/bin/procgateway PATH/gateway.block
go tool pprof -top .cache/proc/bin/procgateway PATH/gateway.mutex
go tool pprof -top -alloc_space .cache/proc/bin/procgateway PATH/gateway.allocs
go tool trace -d=parsed PATH/gateway.trace
```

Set `TMPDIR`, `GOTMPDIR`, and `GOCACHE` under `.cache/` before invoking Go tools directly.
The Makefile already does this for builds.
`--batch-delay` tests existing collection waits without changing production defaults.
Only compare identical unprofiled matrices before and after each code change.
