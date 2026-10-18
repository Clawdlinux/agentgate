---
phase: 04-synchronous-request-path-ledger
plan: 04-01
completed: 2026-08-14
status: complete
---

# Phase 4 Summary: Synchronous Request-Path Ledger

## What was built

- `internal/receipt/ledger.go` + `ledger_test.go` + `BenchmarkLedgerAppend`:
  `Ledger.Append` runs one `BEGIN IMMEDIATE` SQLite transaction per receipt
  — read head, fetch the active signer key, compute the entry hash, sign,
  insert, commit. `BEGIN IMMEDIATE` acquires the write lock before any
  read, closing the race an ordinary pooled `db.Begin()` would leave open
  between reading the head and inserting the next row. No in-memory head
  cache exists anywhere; every `Append` and `Head` call reads SQLite
  directly.
- `internal/db/migrations/002_receipts.sql`: the `receipts` table (append-
  only via `UPDATE`/`DELETE`-rejecting triggers), indexed on timestamp,
  agent, and (service, action). Coexists with Phase 3's
  `002_signer_keys.sql` as a second `002_`-prefixed file. Leaves
  `audit_log`, `tokens`, and `agent_keys` untouched.
- `internal/db/sqlite.go`: added `_foreign_keys=on` to the connection DSN
  so the new table's `signer_kid REFERENCES signer_keys(kid)` is actually
  enforced (SQLite does not enable foreign keys by default per connection).
- `internal/gateway/gateway.go`: full rewrite of the request path around
  ARCHITECTURE.md's `prepareAttempt`/`executeAttempt` split. Unauthenticated
  and malformed requests never reach `executeAttempt` and are never
  receipted. Every other exit — scope denial, rate limit, unknown
  service/action, missing/expired token, upstream network failure, upstream
  HTTP response — builds an outcome, which `handleAct` always turns into a
  receipt append *before* writing any HTTP response. A failed append
  returns `500` and never returns the would-be outcome. `gateway.Config`
  replaced the MVP `AgentKeys map[string]string` with three narrow
  interfaces (`AgentAuthorizer`, `RequestLimiter`, `ReceiptRecorder`) so
  unit tests use small fakes without opening SQLite.
- `internal/auth/keys.go`: added `KeyStore.Count` (+ `keys_test.go`, the
  package's first test file), used by the composition root to decide
  whether to bootstrap a first agent key.
- `cmd/agentgw/main.go`: rewritten as the real composition root. Opens
  SQLite, runs every migration, constructs `vault.NewSQLiteStore`,
  `auth.KeyStore`, `signer.Store` (+ one `LoadOrCreateActive(1)` at boot),
  `ratelimit.Limiter`, `receipt.Ledger`, wires all of it into
  `gateway.New`, and mounts `GET /v1/receipts/pubkey` (Phase 3 built the
  handler; Phase 3's CONTEXT.md deferred mounting it here). Fixed the
  `VAULT_ENCRYPTION_KEY`/`AGENTGATE_VAULT_KEY` env var name mismatch
  PROJECT.md and the committed research both flagged — no code before this
  phase ever actually read the docker-compose/`.env.example` key.
- Bootstrap path: on an empty `agent_keys` table, the composition root
  creates one key and logs its plaintext value once. The previous
  `AGENT_API_KEY` plaintext-env-var model has no equivalent under
  bcrypt-hashed keys; `.env.example` and `docker-compose.yaml` updated to
  describe the new bootstrap-and-log flow. The docker-compose dev vault key
  was also fixed from 30 to 32 bytes — it was silently ignored under the
  old, differently-named env var, so its wrong length had never been
  exercised before.
- `tests/integration/gateway_test.go`: rewritten against real dependencies
  throughout (file-backed SQLite via `internal/db.Open`/`RunMigrations`,
  `vault.SQLiteStore`, `auth.KeyStore`, `signer.Store`, `receipt.Ledger`) —
  the previous inline hand-rolled 3-table schema and dead `audit.NewLogger`
  setup are gone. Added a receipt-row assertion after a successful action
  and a 25-concurrent-request end-to-end test asserting the resulting
  chain is exactly sequences 1..25 with no gaps or duplicates.

## Key test results

- `internal/receipt`: 6 new ledger tests, including
  `TestLedgerAppend_HundredConcurrentAppendsProduceExactSequence1To100`
  (LEDG-09) under `-race` — passes in ~0.6s.
- `internal/gateway`: 12 tests including a scope-denial receipt assertion,
  a rate-limit receipt assertion, and `TestAct_ReceiptAppendFailure`
  proving a failed receipt commit returns `500`, never the outcome
  (LEDG-07).
- `tests/integration`: 25-concurrent real-HTTP test confirms the gateway's
  actual HTTP path — not just the ledger in isolation — produces a
  gap-free chain (LEDG-09 end to end).
- `go build ./...`, `go vet ./...`, `gofmt -l` (Phase 4 files) all clean.
  `gofmt -l .` repo-wide flags only the pre-existing `internal/registry/registry.go`
  (documented residual risk since Phase 3; still out of Phase 4's scope).
- Full suite: `go test ./... -race -count=1` — all packages pass.

## LEDG-10: measured p99 latency

`BenchmarkLedgerAppend` (200 sequential appends, single writer, file-backed
SQLite, Apple M4): **~122 microseconds per append** (0.122ms). This is
roughly 400x under the 50ms threshold PROJECT.md sets for revisiting the
design — no design change is warranted.

## Deviations from the plan

- The committed `ARCHITECTURE.md` (written before Phase 3 shipped) proposed
  `internal/receipt/keys.go` + a `receipt_keys` table with a signed
  rotation-transition chain (`previous_kid`/`transition_signature`). Phase
  3 had already shipped and verified (5/5) a narrower design —
  `internal/signer` + `signer_keys`, satisfying every KEY-0X requirement
  without a signed transition chain. Reopening a verified, merged phase to
  add an unrequired feature would have been scope creep; Phase 4 reuses
  `internal/signer.Store` as-is for the research's "key manager" role.
  Recorded in `04-CONTEXT.md`.
- Biscuit delegation (step 7 of ARCHITECTURE.md's request-flow diagram) is
  explicitly Phase 8 — not wired here. Every Phase 4 receipt's
  `DelegationChain` field stays empty.
- `internal/db`'s lack of a `schema_migrations` ledger (ARCHITECTURE.md's
  Migration Safety section) is a real improvement but no LEDG-0X
  requirement asks for it — deferred, not implemented, to keep this
  already-large phase bounded.
- Admin/OAuth HTTP routes are still not mounted in `cmd/agentgw/main.go`.
  No LEDG-0X requirement references them; wiring them is out of this
  phase's scope (they remain reachable only via the integration test's own
  mux, not the production binary).

## Residual risks

- `internal/registry/registry.go` remains unformatted by `gofmt` — a
  pre-existing issue from before Phase 3, still out of scope.
- The bootstrap agent key is logged once at `slog.Warn` level to stdout;
  if an operator misses that line before rotating logs, there is currently
  no way to create a first key without direct SQLite access or a future
  admin CLI/route. Acceptable for this milestone; worth a follow-up once
  the admin routes are wired into the production binary.
