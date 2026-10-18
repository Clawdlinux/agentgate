# Phase 0: end-to-end act overhead

**Verdict: FAIL overall. The `<1 ms p99` exit gate is not met across the tested load levels.**

Only the original concurrency-1 run with a 0 ms upstream passes.
All 6 fixed-arrival-rate scenarios also miss the target.
No gateway production code or tuning changed.
These results don't establish performance on an actual 4-vCPU machine.

## Verdict by load level

| Load level | 0 ms upstream | 5 ms upstream |
| --- | --- | --- |
| Original closed-loop c1 | PASS, p99 0.3410 ms | FAIL, p99 2.078 ms |
| Open-loop 25%, 5,403.5 requests/s | FAIL, p99 6.996 ms | FAIL, p99 2.329 ms |
| Open-loop 50%, 10,807 requests/s | FAIL, p99 111.029 ms | FAIL, p99 17.977 ms |
| Open-loop 75%, 16,210.5 requests/s | FAIL, p99 37.918 ms | FAIL, p99 298.834 ms |
| Original closed-loop c16 | FAIL, p99 3.418 ms | FAIL, p99 4.277 ms |
| Original closed-loop c64 | FAIL, p99 6.217 ms | FAIL, p99 4.006 ms |

The original c16 and c64 runs are saturation probes, not measurements of isolated incremental processing cost.
Their loaded tail latency includes queueing.
The fixed-rate runs retain queueing but stop coupling the next arrival to the previous completion.
No tested fixed rate establishes a passing load range on this host.
The percentage denominator is the highest original c64 throughput, 21,614 requests/s.
It isn't a measured CPU-utilization percentage or a guaranteed capacity under a different workload.
In particular, the original 5 ms c64 run reached only 10,270 requests/s.
The common 50% and 75% offered rates exceed that separate closed-loop throughput.

## Host and revision

Measured on 2026-10-18.

| Item | Value |
| --- | --- |
| Gateway base | `origin/main`, `2f81c85a2364622fb462f1cf5a646db476028eb7` |
| Benchmark branch | `perf/phase0-exit-bench` |
| `sysctl machdep.cpu.brand_string` | `Apple M4` |
| `sysctl hw.ncpu` | `10` |
| `sysctl hw.memsize` | `17179869184` bytes, 16 GiB |
| `sw_vers` | macOS `27.0.1`, build `26A434` |
| `go version` | `go version go1.26.0 darwin/arm64` |
| `GOMAXPROCS` | `4` |
| Storage | Real SQLite files on the host filesystem, under `.cache/tmp` |

`GOMAXPROCS=4` emulates 4 concurrent Go execution slots on Apple M4.
It doesn't reserve CPUs, impose VM quotas, or isolate external processes.
The client and mock share those slots with the gateway.
This is a local development host, not a dedicated Linux performance runner.
The original closed-loop table is unchanged from commit `a7a1a8c588029a47ee0ad85ebc9d927eedd28e27`.
The open-loop and wakeup comparison were added afterward on the same host.
Only 1 complete measured run was recorded per final scenario. No repeated-run confidence intervals are available.

## Measurement

`BenchmarkHandleActE2E` uses the real `gateway.Server` over an `httptest.NewServer` loopback listener.
It exercises authentication and authorization, then proxies to a second loopback listener.
The stack uses:

- A fresh on-disk SQLite database and every real migration.
- A real HMAC `auth.KeyStore`, with its default 30s cache TTL.
- A real encrypted SQLite vault behind `vault.CachedStore`, with its default 60s cache TTL.
- A real signing-key store and receipt `Ledger`.
- Default group commit: 256 maximum receipts per transaction, with no collection delay.
- The production upstream pool, with no custom client or transport injected.
- Active key, tenant, and service rate buckets, each configured for 1,000,000 requests/s and a 1,000,000 burst.
- A tenant inflight limit of 1,024.

There is 1 scoped agent key, 1 bearer token, and 1 service with a POST action.
The request is about 100 bytes. The upstream response is `{"ok":true}`.
The token is unexpired. OAuth refresh and delegation aren't part of this workload.
Info logging still performs JSON encoding and discards its output.
The added benchmark harness retains error-level logs for failure diagnosis.
Terminal, file, and remote log-sink costs are excluded.
The gateway's default upstream idle-per-host limit remains 32, including at concurrency 64.

Each scenario runs 256 warmup requests, then 20,000 measured requests.
Workers issue requests without think time, with at most 1 outstanding request per worker.
This is closed-loop load. It doesn't model fixed arrival rates or avoid coordinated omission.
Warmup establishes credentials and connections. Cache TTL refreshes can still occur during longer runs.
All measured requests succeeded. Each completed request had an acknowledged signed receipt before its response.
Checks confirm receipt count, sequential head, successful status, policy, and signature length.

The per-request client timer starts immediately before `http.Client.Do`.
It ends after reading and closing the entire gateway response body.
Request creation and client-side response JSON decoding are outside that timer.
Each upstream handler measures its actual elapsed duration through its response write.
A buffered channel per worker associates this duration with that worker's outstanding request.
The channel operation itself isn't subtracted.

```text
overhead[i] = client_elapsed[i] - measured_upstream_handler_elapsed[i]
```

Subtraction happens before sorting. Percentiles use the nearest-rank definition.
Upstream sleep overshoot is measured, not replaced with the requested 5 ms delay.
Client overhead, both loopback paths, and gateway queueing remain in the estimate.
Response flushing after the upstream handler returns also remains in the estimate.
These numbers are conservative end-to-end overhead estimates, not isolated handler CPU times.
Throughput is successful measured requests divided by measured wall time, including upstream time.
Go's `ns/op` is aggregate wall time per operation. It isn't per-request latency under concurrency.

## Original closed-loop results

All latency values are milliseconds. Throughput is requests/s.
The sample count is 20,000 in every row.

| Upstream delay | Concurrency | Overhead p50 | Overhead p90 | Overhead p99 | Overhead p99.9 | Throughput | `<1 ms p99` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 0 ms | 1 | 0.1453 | 0.1849 | 0.3410 | 2.210 | 5,918 | PASS |
| 0 ms | 16 | 0.7516 | 1.198 | 3.418 | 4.315 | 17,832 | FAIL |
| 0 ms | 64 | 2.716 | 3.857 | 6.217 | 7.084 | 21,614 | FAIL |
| 5 ms | 1 | 0.5777 | 0.8749 | 2.078 | 6.938 | 146.4 | FAIL |
| 5 ms | 16 | 1.139 | 1.698 | 4.277 | 5.216 | 2,505 | FAIL |
| 5 ms | 64 | 0.8710 | 2.166 | 4.006 | 5.186 | 10,270 | FAIL |

The measured upstream means show why subtracting a nominal delay would be misleading.

| Upstream delay | Concurrency | Measured upstream mean, ms | Total request p99, ms |
| --- | ---: | ---: | ---: |
| 0 ms | 1 | 0.002202 | 0.3458 |
| 0 ms | 16 | 0.001793 | 3.419 |
| 0 ms | 64 | 0.001480 | 6.222 |
| 5 ms | 1 | 6.158 | 8.678 |
| 5 ms | 16 | 5.194 | 9.424 |
| 5 ms | 64 | 5.059 | 9.091 |

The total request p99 isn't used to calculate overhead p99.
Different requests can occupy those percentile ranks.
At 20,000 requests, p99.9 describes about 20 tail samples.
Treat it as a tail indicator, not a precise service-level guarantee.

## Fixed-arrival-rate results

`BenchmarkHandleActOpenLoop` offers evenly spaced arrivals, independent of earlier request completion.
Both delays use the same reference throughput: 21,614 requests/s from the original 0 ms c64 run.
The offered rates are exactly 5,403.5, 10,807, and 16,210.5 requests/s.
Each row has 256 warmup requests and 20,000 measured requests.
Every measured request completed successfully, with its signed receipt acknowledged.
No scheduled requests were dropped.

```text
scheduled[i] = epoch + i / offered_rate
overhead[i] = response_body_completed[i] - scheduled[i] - measured_upstream_handler_elapsed[i]
```

Each request gets a unique upstream timing slot.
Late arrivals keep their original timestamp. The schedule never resets or skips work to catch up.
This avoids coordinated omission from waiting for previous completions.
Client request creation and connection-pool waiting also remain in scheduled latency.
The load generator yields with `runtime.Gosched` while waiting for the next deadline.
It avoids sub-millisecond sleeps, but competes within the same `GOMAXPROCS=4` budget.
This isn't an isolated external load generator.

All latency values below are milliseconds.
Goodput includes the final queue drain, so it can fall below the offered rate.
Every row fails the `<1 ms p99` gate.

| Delay | Reference load | Offered requests/s | Overhead p50 | Overhead p90 | Overhead p99 | Overhead p99.9 | Goodput, requests/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0 ms | 25% | 5,403.5 | 0.2388 | 1.490 | 6.996 | 18.072 | 5,403 |
| 0 ms | 50% | 10,807 | 1.014 | 73.218 | 111.029 | 115.567 | 10,459 |
| 0 ms | 75% | 16,210.5 | 2.116 | 22.192 | 37.918 | 41.142 | 15,787 |
| 5 ms | 25% | 5,403.5 | 0.1698 | 0.5189 | 2.329 | 2.998 | 5,393 |
| 5 ms | 50% | 10,807 | 0.5364 | 3.153 | 17.977 | 29.768 | 10,777 |
| 5 ms | 75% | 16,210.5 | 198.188 | 285.943 | 298.834 | 302.655 | 13,004 |

The generator records lateness through the actual `http.Client.Do` invocation.
Connection-pool waiting occurs after that point. It remains in overhead, not in the lateness column below.

| Delay | Reference load | Client-start lateness p99, ms | Measured upstream mean, ms | Total scheduled latency p99, ms |
| --- | ---: | ---: | ---: | ---: |
| 0 ms | 25% | 0.2471 | 0.002873 | 6.999 |
| 0 ms | 50% | 0.2990 | 0.003080 | 111.044 |
| 0 ms | 75% | 0.5686 | 0.002674 | 37.921 |
| 5 ms | 25% | 0.01906 | 5.009 | 7.332 |
| 5 ms | 50% | 0.2614 | 5.081 | 23.886 |
| 5 ms | 75% | 1.523 | 5.343 | 303.992 |

The client permits up to 128 TCP connections. Pending requests aren't capped or dropped.
The original 64-worker limit doesn't constrain arrivals.
Client-pool and gateway queues remain visible in each request's scheduled latency.
This connection limit can affect congested rows, so these aren't isolated server-service-time measurements.
The gateway's upstream pool and admission settings remain unchanged.

An initial harness allowed 1,024 client connections and encountered loopback connection resets during bursts.
Those incomplete runs aren't valid percentile measurements and aren't used above.
Bounding connection creation avoids that local failure without reducing the scheduled request count.
The completed run took 15.030s across the 6 scenarios.
The non-monotonic tails show run sensitivity. They don't justify interpolating a passing load level.

## The 5 ms c1 tail

`BenchmarkHandleActWakeup` compares sleeping and busy-spinning inside the otherwise identical upstream handler.
Both variants use concurrency 1, a 5 ms requested delay, and 20,000 measured requests.
Actual upstream elapsed time is still subtracted per request.
Neither variant changes gateway code, receipt behavior, or rate limits.

All latency values below are milliseconds.

| Upstream wait | Overhead p50 | Overhead p90 | Overhead p99 | Overhead p99.9 | Upstream mean | Throughput, requests/s | Gate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Sleep, repeated diagnostic | 0.5247 | 0.8235 | 1.855 | 4.459 | 6.175 | 147.6 | FAIL |
| Busy spin | 0.3280 | 0.4522 | 1.371 | 4.945 | 5.005 | 185.7 | FAIL |

The comparison took 249.651s. The original sleeping c1 row remains visible above.
Busy spin lowers p99 by about 26%, but it still misses the gate.
Its p99.9 is worse, so this isn't evidence that spinning fixes tail latency.
The actual sleep overshoot was already subtracted. It doesn't directly account for the remaining overhead.
macOS idle wakeup, scheduling, or power-state changes remain hypotheses, not established causes.
Busy spin keeps a Go execution slot active and changes the host's execution pattern.
That prevents this comparison from isolating idle wakeup alone.
Execution traces and power-state measurements are needed before attributing the remaining tail.
Don't subtract an assumed platform penalty from the reported gate result.

## Commands

Run from the repository root.
Scratch files, Go caches, and profiles stay inside ignored `.cache/`.

```sh
sysctl machdep.cpu.brand_string hw.ncpu hw.memsize
sw_vers
go version

make bench-e2e
```

The Makefile target defaults to 20,000 requests per scenario and `GOMAXPROCS=4`.
It sets `TMPDIR`, `GOTMPDIR`, and `GOCACHE` inside `.cache/`.
Its Go command is:

```sh
GOMAXPROCS=4 go test -tags=bench ./bench -run '^$' \
  -bench '^BenchmarkHandleActE2E$' -benchtime=20000x -count=1
```

The benchmark passed its functional checks in 156.182s.
Passing those checks doesn't mean the latency target passed.
For a quick functional run, use `make bench-e2e BENCHTIME=64x`.
Don't use that small sample for tail claims.
Select a scenario with `BENCH_FILTER`, for example:

```sh
make bench-e2e BENCH_FILTER='^BenchmarkHandleActE2E/delay=0s/c=64$$'
```

The doubled dollar sign preserves the regex anchor through Make expansion.

For the new fixed-arrival matrix:

```sh
make bench-e2e-open-loop
```

This retains the existing `bench-e2e` target and its closed-loop defaults.
The new target uses the same cache paths, `GOMAXPROCS=4`, and 20,000-request default.
Its Go command is:

```sh
GOMAXPROCS=4 go test -tags=bench ./bench -run '^$' \
  -bench '^BenchmarkHandleActOpenLoop$' -benchtime=20000x -count=1
```

For the sleep versus spin comparison:

```sh
make bench-e2e BENCH_FILTER='^BenchmarkHandleActWakeup$$'
```

Its Go command is:

```sh
GOMAXPROCS=4 go test -tags=bench ./bench -run '^$' \
  -bench '^BenchmarkHandleActWakeup$' -benchtime=20000x -count=1
```

## Failure profile

The profile targets the failed 0 ms, concurrency-64 scenario.
It uses 200,000 requests to collect more samples.
These instrumented measurements aren't used in the results table.
Setup, warmup, and untimed benchmark checks also appear in the process-wide CPU profile.
The larger run also grows the receipt database further.

```sh
mkdir -p .cache/tmp .cache/go-tmp .cache/go-build
export TMPDIR="$PWD/.cache/tmp"
export GOTMPDIR="$PWD/.cache/go-tmp"
export GOCACHE="$PWD/.cache/go-build"
export GOMAXPROCS=4

go test -tags=bench ./bench -run '^$' \
  -bench '^BenchmarkHandleActE2E/delay=0s/c=64$' \
  -benchtime=200000x -count=1 \
  -cpuprofile .cache/act-c64.cpu -o .cache/bench.test
go tool pprof -top -nodecount=15 .cache/bench.test .cache/act-c64.cpu
go tool pprof -top -cum -nodecount=80 .cache/bench.test .cache/act-c64.cpu
```

Profile duration was 12.34s, with 23.08s of attributed samples.
The top 5 flat costs were:

| Function | Flat samples | Share | What to investigate |
| --- | ---: | ---: | --- |
| `syscall.rawsyscalln` | 20.63s | 89.38% | Socket and SQLite system calls |
| `runtime.kevent` | 0.75s | 3.25% | Network poller activity |
| `runtime.pthread_kill` | 0.31s | 1.34% | Runtime signals and preemption |
| `runtime.usleep` | 0.30s | 1.30% | Runtime scheduling waits |
| `runtime.pthread_cond_wait` | 0.23s | 1.00% | Runtime thread parking |

The cumulative view attributes 37.05% to `receipt.(*Ledger).commitBatch`, including its children.
SQLite execution accounts for 36.66%, including 26.30% under `receipt.insertReceipt`.
Socket writes account for 32.58%. Socket reads account for 20.62%.
SQLite page reads account for 19.41% under `_unixRead`.
These cumulative paths overlap. Their percentages must not be added.
The signing callback encloses database work. Its cumulative share doesn't isolate Ed25519 signing cost.
macOS syscall attribution doesn't prove where wall-clock tail latency originates.
The profile includes the in-process client and upstream, not just gateway CPU work.

### Follow-ups, without tuning this change

1. Repeat on a dedicated 4-vCPU Linux runner, then run k6 with a separate load generator.
2. Capture execution traces and block profiles to separate receipt queueing from scheduler stalls.
3. Inspect group-commit batch sizes and SQLite page I/O under sustained ledger growth.
4. Measure socket and connection-pool costs, including the default 32 idle connections at concurrency 64.
5. Investigate timer and scheduler jitter in the 5 ms workload. Preserve measured upstream subtraction.

Separate client-pool waits from gateway and receipt queueing before attributing the open-loop tails to one component.
Keep synchronous receipt acknowledgment and durability guarantees unchanged during any later tuning.

## k6 status

`which k6` found no executable. k6 wasn't installed.
**k6 performance numbers are pending.**
`node --input-type=module --check < bench/k6/act.js` passed.
That validates JavaScript syntax only, not k6 execution.
The script includes a `<1 ms p99` overhead threshold and failure checks.
See [local setup](k6/README.md) for the mock upstream and real gateway provisioning commands.
An HTTP smoke check verified that provisioning flow against both local server binaries.
Both servers were stopped afterward.

## Validation

With the cache variables above and `GOMAXPROCS=4`:

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go vet -tags=bench ./bench` | PASS |
| `go test -race ./...` | PASS, all normal packages |
| `go test -tags=bench ./bench ./bench/mockupstream -run Test -count=1` | PASS |
| `make bench-e2e BENCHTIME=64x` | PASS, 6 functional scenarios |
| `make bench-e2e` | PASS, 120,000 measured requests; latency target FAIL |
| `make bench-e2e-open-loop` | PASS, 120,000 measured requests; all 6 latency gates FAIL |
| `make bench-e2e BENCH_FILTER='^BenchmarkHandleActWakeup$$'` | PASS, 40,000 measured requests; both latency gates FAIL |
| Profile command above | PASS |
| `node --input-type=module --check < bench/k6/act.js` | PASS, syntax only |

The `bench` build tag excludes the heavy benchmark from normal race-test runs.
Mock-upstream tests remain included in `go test -race ./...`.
Build, both vet commands, race tests, and tagged helper tests were rerun after the open-loop addition.
The existing closed-loop 64-request smoke matrix was rerun and passed.
The full closed-loop table and CPU profile remain the original measurements, not new reruns.
No `.cache/` files belong in the commit.
