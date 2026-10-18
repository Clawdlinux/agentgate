# Separate-process gateway tail investigation

**Current verdict: FAIL. Separate processes don't establish `<1 ms p99` at moderate fixed arrival rates.**

The allocation fix reduces gateway bytes/request by about 24% in a 5,000 requests/s comparison.
It doesn't resolve the tail gate.
Only the 0 ms, closed-loop c1 case passes in the new full matrix.
All fixed-rate cases fail in both after runs.
Linux 4-vCPU validation remains pending.

## New method and limits

Measured on 2026-10-18, starting from `origin/main`, `19084f8`.
The host remains Apple M4, 10 cores, 16 GiB, macOS `27.0.1`, Go `1.26.0`.
See [PROC.md](PROC.md) for the complete setup and timing contract.

`make bench-proc` builds 3 Go binaries and supervises separate processes:

| Process | Implementation | GOMAXPROCS |
| --- | --- | ---: |
| Gateway | `bench/procgateway`, wiring the real gateway stack | 4 |
| Upstream | `bench/mockupstream` | 2 |
| Load generator | `bench/loadgen` | 2 |

The gateway retains real authentication, scoped authorization, active rate limits, encrypted caches, and the signed SQLite ledger.
No delegation token is presented. No production delegation check changed.
SQLite startup reports WAL and `synchronous=2`, meaning FULL.
Both checked database connections retain WAL, FULL, foreign keys, and the 5,000 ms busy timeout.
The new durability regression test enforces those settings on 2 simultaneous connections.
No receipt can acknowledge success before COMMIT returns.
Batch size remains 256. Collection delay remains 0.
The production upstream transport remains unchanged.
Info logs still undergo JSON encoding, with sink writes discarded.

Each case uses a fresh database, 256 warmup requests, and 20,000 measured requests.
The mock supplies measured upstream handler nanoseconds inside its JSON response.
This subtracts actual sleep overshoot, not a nominal 5 ms.
Response encoding and network writes remain in overhead.
Open-loop time starts at the original scheduled arrival, including late dispatch and connection-pool waits.
Closed-loop time starts immediately before `Client.Do`.
Both end after reading and closing the response body.
Subtraction precedes sorting. Percentiles use nearest rank.
The client caps connections at 128. This bounds connection bursts, not arrivals.
Every scheduled request still runs. Its original timestamp never resets.
Failed samples are excluded from latency percentiles.
Any failed request marks the summary invalid and emits `p99_ms: null`.
Other available percentiles describe successful requests only. They aren't a passing gate for an invalid row.
Receipt-count mismatches also invalidate the row and suppress p99.

The first full before and after matrices each completed 240,000 measured requests without errors.
Every case's ledger head advanced by exactly 20,000.
The repeated after fixed-rate matrix also completed all 160,000 requests.
Initial 1,024-connection experiments encountered loopback connection resets.
Those failed experiments aren't passing evidence and aren't included in the main comparison.

`GOMAXPROCS=4` isn't CPU isolation, pinning, or a VM quota.
Background CPU activity was observed during this shared-host investigation.
Other test workloads were deferred when coordination allowed.
Interactive host processes remained active.
These runs have no isolated-host confidence interval.
Don't treat a better single run as proof of a causal p99 improvement.
The earlier shared-generator method was flawed, but its FAIL wasn't proved to be method-only.

## Separate-process before and after

All latency values are milliseconds. Throughput includes measured upstream time.
Before uses the unoptimized production paths, with the new process harness.
After includes `d15cf71`, the receipt allocation fix.
The repeat reruns every fixed rate after that same production fix.

| Upstream | Load | Before p99 | After p50 | After p99 | Repeat after p99 | After requests/s | After gate |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| 0 ms | 1,000/s | 5.785 | 0.250 | 1.952 | 4.535 | 1,000 | FAIL |
| 0 ms | 2,500/s | 33.733 | 0.218 | 5.586 | 33.822 | 2,500 | FAIL |
| 0 ms | 5,000/s | 50.720 | 0.193 | 2.734 | 8.194 | 5,000 | FAIL |
| 0 ms | 10,000/s | 73.768 | 0.419 | 5.270 | 69.471 | 9,999 | FAIL |
| 0 ms | Closed c1 | 0.690 | 0.165 | 0.438 | Not repeated | 5,274 | PASS, single run |
| 0 ms | Closed c16 | 5.611 | 0.845 | 3.643 | Not repeated | 16,312 | FAIL |
| 5 ms | 1,000/s | 21.760 | 0.267 | 2.304 | 6.233 | 1,000 | FAIL |
| 5 ms | 2,500/s | 5.590 | 0.293 | 4.457 | 9.699 | 2,498 | FAIL |
| 5 ms | 5,000/s | 45.674 | 22.866 | 1,702.730 | 59.228 | 3,777 | FAIL |
| 5 ms | 10,000/s | 149.782 | 0.705 | 25.411 | 100.367 | 9,790 | FAIL |
| 5 ms | Closed c1 | 4.802 | 0.506 | 2.302 | Not repeated | 147.6 | FAIL |
| 5 ms | Closed c16 | 4.434 | 1.073 | 4.113 | Not repeated | 2,477 | FAIL |

The 1.7-second after tail isn't discarded as an outlier.
Its slowest 1% averages 1,715.248 ms overhead.
Receipt append accounts for 58.481 ms, or 3.41%, of that conditional mean.
Transport, request acceptance, client connection waits, and response completion account for 1,655.857 ms, or 96.54%.
Dispatch accounts for 0.0067 ms. Authentication accounts for 0.0016 ms.
Other gateway work accounts for 0.902 ms.
That run predates explicit client-connection timing, so its residual can't be split retrospectively.
The subsequent same-rate run still fails, with p99 59.228 ms.
This variability prevents an honest claim that receipt allocation alone fixes the observed p99.

Local evidence directories, all ignored:

| Evidence | Directory under `.cache/proc/` |
| --- | --- |
| Full before matrix | `bounded-before-c9598240` |
| Full after matrix | `after-9e36bcec` |
| Repeated after fixed rates | `repeat-after-69b46760` |
| Controlled 5,000/s allocation baseline | `controlled-before-f9aabf7a` |
| Before 5,000/s profile | `prof-before-c5807e20` |
| After profiles, 0 ms and 5 ms | `prof-after-0f54c36b` |
| Foreground-policy control | `foreground-f8e7de70` |
| Profiles with client-connection timing | `latest-prof-892308cc` |

These private files aren't committed. Each directory contains CSV samples and JSON summaries.
Profiles remain local, with the databases.

### macOS foreground-policy control

`taskpolicy -B -p PID` succeeded before each gateway warmup.
This clears background policy. It doesn't pin CPUs or isolate the generator.
All 4 cases completed without errors, with 20,000 committed receipts each.
These additional unprofiled controls still miss the gate.
They aren't evidence that foreground policy worsens latency. Host activity wasn't controlled.

| Upstream | Offered requests/s | Overhead p99, ms | Dispatch p99, ms | Gate |
| --- | ---: | ---: | ---: | --- |
| 0 ms | 1,000 | 85.127 | 36.857 | FAIL |
| 0 ms | 5,000 | 5.352 | 0.0408 | FAIL |
| 5 ms | 1,000 | 10.809 | 0.0610 | FAIL |
| 5 ms | 5,000 | 65.628 | 0.9912 | FAIL |

The first control recorded 204.066 ms of aggregate gateway GC pauses.
Its large dispatch lag also demonstrates scheduler pressure outside receipt COMMIT.
No assumed platform penalty is subtracted from any gate.

## Tail contributors

Profiles are gateway-only. The generator and mock run in different processes.
CPU, block, mutex, allocation, execution trace, and `GODEBUG=gctrace=1` evidence were collected.
Request IDs correlate client samples with receipt transactions and committed replies.
Profiled numbers aren't substituted into the gate table.

### Correlated receipt-heavy tail

The quieter after profile uses 5,000/s and 0 ms upstream.
Its overhead p99 is 3.723 ms.
The slowest 200 requests average 4.503 ms overhead.
The following partition uses those same requests. It doesn't add unrelated percentile ranks.

| Contributor | Conditional mean, ms | Share of that tail mean |
| --- | ---: | ---: |
| Load-generator dispatch | 0.023 | 0.51% |
| Authentication, including key cache | 0.001 | 0.03% |
| Receipt append, including all waits | 4.137 | 91.87% |
| Other gateway work, minus upstream time | 0.119 | 2.65% |
| Transport and response completion | 0.222 | 4.94% |

Client-connection timing wasn't recorded in that profile's CSV.
Its transport row therefore includes the client connection wait.
Upstream `httptrace` was available: reuse was 99.975%, with connection wait p99 0.0093 ms.
Its maximum upstream connection wait was 0.355 ms.
That doesn't support a connection-pool change as the primary fix for this receipt-heavy tail.

All 200 tail requests matched their committed receipt transactions.
Within their receipt wait, these trace spans averaged:

| Receipt contributor | Conditional mean, ms |
| --- | ---: |
| Queueing, preflight, and writer acquisition | 2.470 |
| Head read | 0.0285 |
| Signing the entire batch | 0.4065 |
| Inserting the entire batch | 0.5500 |
| SQLite COMMIT | 0.5774 |
| Other transaction work | 0.0251 |
| Reply scheduling after the transaction | 0.0790 |

Queueing accounts for about 54.9% of the full tail mean.
COMMIT accounts for about 12.8%.
Batch signing and inserts account for about 9.0% and 12.2%.
These are wall-clock spans. They include descheduling, not just crypto or SQLite CPU.
Each caller waits for its entire batch, so batch work is deliberately attributed to every waiting caller.
Trace timer boundaries differ slightly from the response-header timer.

Across all transactions, rather than only the tail requests:

| Trace region | Before p99, ms | After p99, ms |
| --- | ---: | ---: |
| Head read | 0.0473 | 0.0623 |
| Receipt signing | 0.0509 | 0.0588 |
| Receipt insert | 0.0922 | 0.1228 |
| SQLite COMMIT | 0.2134 | 0.2897 |
| Entire transaction | 0.4897 | 0.7171 |
| Caller append wait | 12.0573 | 3.4212 |

This is a noisy, single-run comparison, not evidence of an allocation-driven tail improvement.
Caller append p99 fell while transaction p99 increased in the same table.
Don't attribute that movement to `d15cf71`.

After COMMIT p50 is 0.0628 ms, with maximum 4.186 ms.
The automatic WAL checkpoint threshold remains 1,000 pages.
The existing macOS `fullfsync` setting remains 0. SQLite synchronous mode remains FULL.
COMMIT includes SQLite's WAL sync and any automatic checkpoint work.
A separate kernel fsync-duration distribution wasn't obtained.
macOS `fs_usage` refused unprivileged execution. No privileged trace was run.
Don't describe these COMMIT wall times as isolated fsync syscall times.
Linux syscall-level sync validation remains pending.

### Locks, GC, JSON, logging, and connections

The receipt-heavy after CPU profile spans 4.31s, with 4.69s of attributed CPU samples.
System calls account for 59.91% of samples.
`Ledger.commitBatch` accounts for 50.75% cumulatively.
SQLite `_full_fsync` accounts for 6.40% cumulatively.
These paths overlap. CPU shares aren't p99 wall-time shares.
`slog.Info` accounts for 0.64% of CPU samples. Authentication accounts for 0.21%.
JSON encoding wasn't a sampled CPU hotspot in this short run.
JSON decoder buffers still account for 5.69% of sampled allocation bytes.

The block profile attributes 6.26s of aggregate wait to `Ledger.Append`.
It attributes 1.20s to upstream dispatch.
Those are summed goroutine waits, not elapsed process time.
Ledger submission and acknowledgment use channels, not a contended limiter lock.

The mutex profile records 3.12 ms total observed delay over 20,256 requests.
Limiter admission accounts for about 0.32 ms cumulatively. Slot release accounts for 0.34 ms.
The upstream pool accounts for 0.25 ms.
Vault-cache and key-cache locks aren't reported as contention hotspots.
For scale, distributing every observed mutex delay over 200 tail requests averages 0.0156 ms each.
This doesn't justify adding lock sharding here.

The receipt-heavy trace contains 144 GC stop-the-world spans.
Their p99 is 0.146 ms, with maximum 0.148 ms.
Their mean overlap with a tail receipt append is 0.0101 ms.
GC overlap is already inside the receipt spans. Don't add it again.
That overlap is about 0.22% of the conditional tail mean.
It doesn't explain this run's multi-millisecond queueing by itself.

The profiled 5 ms case has upstream connection reuse of 99.131%.
Upstream connection wait p99 is 0.1125 ms, with maximum 7.404 ms.
Its slowest 1% averages 50.926 ms overhead.
Receipt wait contributes 19.96%, dispatch 12.52%, other gateway work 21.38%, and transport completion 46.13%.
Again, its older CSV can't split client connection waits from the last row.

The later instrumented 5 ms profile explicitly records client connection acquisition.
Its slowest 1% averages 61.788 ms overhead:

| Contributor | Share |
| --- | ---: |
| Client connection acquisition and pool queue | 39.25% |
| Receipt append | 22.52% |
| Other gateway work | 18.84% |
| Remaining transport and response completion | 15.64% |
| Dispatch | 3.75% |
| Authentication | 0.01% |

Upstream reuse there is 98.233%, with connection-wait p99 0.431 ms.
Client acquisition includes dialing and pool waiting. It isn't a gateway receipt cost.
All of it remains in the conservative end-to-end gate.
The later 0 ms profile also shows severe scheduler pressure.
Its conditional tail assigns 75.64% to client connection acquisition and 5.44% to receipts.
Its GC pause maximum reaches 47.858 ms. Its dispatch p99 reaches 72.119 ms.
These changing shares are why a single syscall CPU profile couldn't establish the original root cause.
The remaining tail includes host scheduling and transport queueing, alongside the serialized durable writer.
No fixed share is claimed for uninstrumented JSON, logging, or kernel socket substeps.

## Safe fix applied

`d15cf71`, `perf(receipt): trim canonical buffer allocations`:

- Validate preflight drafts through the same `receiptspec.Validate` contract, without encoding an unused preimage.
- Reserve the exact validated preimage length instead of every field's maximum size.

Canonical bytes, receipt hashes, and signatures remain unchanged.
Signing and inserts still finish before synchronous COMMIT and acknowledgment.
Authentication, rate limits, and delegation behavior remain unchanged.
No GC setting, batch delay, transport limit, or SQLite durability setting changed.
Golden vectors and chain verification tests pass.

The microbenchmarks use a compiler overlay for the original encoding and ledger sources.
They run the same benchmark code before and after, on this host's 10 default Go execution slots.
These aren't load-gate measurements.

| Microbenchmark | Before | After |
| --- | ---: | ---: |
| Preflight production path | 489.5 ns, 3,200 B, 1 allocation | 44.23 ns, 0 B, 0 allocations |
| Canonical preimage encoding | 251.7 ns, 3,200 B, 1 allocation | 89.53 ns, 192 B, 1 allocation |

At 5,000/s with 0 ms upstream, a separate controlled before run allocated 25,977 B/request.
The full after run allocated 19,704 B/request, about 24.2% less.
That control's p99 was 19.950 ms, rather than the earlier matrix's 50.720 ms.
The 50.720 → 2.734 → 8.194 ms p99 movement is shared-host noise, not an effect of `d15cf71`.
That commit saves about 6.2 KB of allocations per request, verified independently.

For that controlled comparison, GC cycles fell from 201 to 184 per 20,000 requests.
Total GC pause time fell from 11.391 ms to 8.984 ms.
Those process counters don't isolate each tail request.
They don't explain the second-scale transport residual.

## Reproduction for this investigation

```sh
make bench-proc BENCH_PROC_ARGS='--label before'
make bench-proc BENCH_PROC_ARGS='--label after'
make bench-proc BENCH_PROC_ARGS='--label repeat-after --closed ""'
make bench-proc BENCH_PROC_ARGS='--label profile --profile --rates 5000 --closed "" --delays 0,5ms'
```

The first command must run before applying `d15cf71`, or with its parent production sources overlaid.
Keep the harness identical during a paired comparison.
Use `.cache/` for overlays, Go scratch files, and profiles.
Run `go test` allocation benchmarks with `-run '^$' -benchmem`.
The benchmark names are `BenchmarkLedgerPreflight` and `BenchmarkCanonicalHashInput`.
See [PROC.md](PROC.md) for profile decoding and correlated tail analysis.

## Investigation commits and validation

These IDs identify the measured pre-rebase commits.

| Commit | Change |
| --- | --- |
| `8632eb0` | Separate-process harness and receipt trace regions |
| `d770739` | Bound client connection bursts and retain failed-case evidence |
| `36164ba` | Record gateway allocation and GC deltas |
| `d15cf71` | Reduce receipt preimage allocations without changing signed bytes |
| `9d39315` | Correlate connection and receipt tails, with analyzer regression tests |

The production allocation fix is isolated in `d15cf71`.
`8632eb0` also adds production trace instrumentation in `internal/receipt/ledger.go`.
That instrumentation is a no-op when tracing is off.
The harness commits don't change production transport, authorization, or durability settings.
All commits are signed off and include the requested co-author trailer.
Nothing was pushed.

Final checks use `GOMAXPROCS=4`, with `TMPDIR`, `GOTMPDIR`, and `GOCACHE` inside `.cache/`.
`SEMGREP_SEND_METRICS=on` allows automatic local rule selection.
`SEMGREP_ENABLE_VERSION_CHECK=0` disables the unrelated version check.
Semgrep ran locally. No remote code analysis was enabled.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go vet -tags=bench ./bench` | PASS |
| `go test -race ./...` | PASS, every normal package |
| `go test -tags=bench ./bench ./bench/mockupstream -run Test -count=1` | PASS |
| `python3 -m unittest discover -s bench -p 'test_*.py'` | PASS, 2 analyzer tests |
| `semgrep --config auto --error --quiet ./internal ./cmd` | PASS, exit 0 |
| `make bench-proc BENCH_PROC_ARGS='--label final-smoke --n 64'` | PASS, all 12 functional cases |
| Full before, after, and repeated fixed-rate commands above | PASS functional checks; latency gates FAIL |
| Foreground-policy control | PASS functional checks; all 4 latency gates FAIL |
| Gateway-only CPU, block, mutex, allocation, and trace collection | PASS |

The final smoke evidence is under `.cache/proc/final-smoke-0abec342`.
Small smoke samples aren't latency-gate evidence.
No cache, database, or profile file is committed.

## Remaining work and tuning boundaries

The current evidence doesn't justify limiter sharding or weaker receipt durability.
Don't replace FULL with NORMAL or acknowledge an uncommitted receipt.
Don't drop late arrivals or subtract assumed scheduler penalties.
Keep the zero collection delay until a repeated paired experiment supports a different setting.

`GOGC=200` is an optional deployment experiment when memory headroom allows.
Pair it with a `GOMEMLIMIT` below the pod memory budget.
Leave space for SQLite's native allocations, socket buffers, and the runtime.
Neither setting is hardcoded or claimed as a measured fix here.
GC pause reduction alone hasn't established the p99 gate.

Dedicated Linux validation must isolate the gateway's 4 vCPUs from the generator and mock.
Repeat each fixed rate and both upstream delays.
Capture syscall-level sync timing there, alongside the same per-request receipt and connection spans.
Until then, no moderate-load production p99 pass is claimed.

---

# Historical Phase 0: in-process act overhead

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
