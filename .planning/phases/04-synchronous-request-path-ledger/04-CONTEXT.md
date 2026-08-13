---
phase: 04-synchronous-request-path-ledger
gathered: 2026-08-14
status: ready-for-planning
mode: autonomous (autopilot loop, no interactive user available)
research: .planning/research/ARCHITECTURE.md, FEATURES.md, PITFALLS.md, SUMMARY.md
  (already committed to the repo before this phase started; treated as the
  authoritative Phase 4 research artifact rather than re-derived)
---

<domain>
## Phase Boundary

Wire receipts into the real request path: every authenticated, schema-valid
`/v1/act` attempt commits one signed, gap-free receipt before AgentGate
returns its response. Requirements: LEDG-01 through LEDG-11.

Biscuit delegation authorization (Phase 8) and offline `agentgate-verify`
(Phase 5) are explicitly out of this phase — the request flow diagram in
ARCHITECTURE.md is milestone-wide, not Phase-4-only.

</domain>

<decisions>
## Deviations from the committed research

The committed `ARCHITECTURE.md` was written before Phase 3 shipped and
proposes `internal/receipt/keys.go` + a `receipt_keys` table with
`previous_kid`/`transition_signature` fields for a signed rotation chain.
Phase 3 already shipped and verified (7/7) a narrower, fully KEY-01..07
compliant design: `internal/signer` package + `signer_keys` table, without
a signed transition chain between rotated keys (KEY-06 requires only that
old receipts stay verifiable within their sequence interval — it does not
require the key history itself to be chained/signed).

Reopening a verified, merged phase to add an unrequired transition-signature
feature is scope creep. Phase 4 reuses `internal/signer.Store` as-is for
the "key manager" role the research assigns to `internal/receipt/keys.go`.

## Ledger design (`internal/receipt/ledger.go`)

`Ledger.Append(ctx, Draft) (Receipt, error)` runs one `BEGIN IMMEDIATE`
transaction on a dedicated `*sql.Conn`:

1. `BEGIN IMMEDIATE` — acquires SQLite's write lock immediately, before any
   read, so no other writer can read a stale head between this
   transaction's head-read and its insert. `database/sql`'s pooled
   `db.Begin()` starts a deferred transaction that only upgrades to a write
   lock at the first write — that gap is exactly the race LEDG-08/LEDG-09
   must not have. `_busy_timeout=5000` (already set in `db.Open`'s DSN)
   makes concurrent `BEGIN IMMEDIATE` calls wait and retry rather than
   error immediately.
2. Read the current head (`seq`, `entry_hash`) inside the transaction, or
   `(0, zero-hash)` if the table is empty.
3. Fetch the active signer key via `signer.Store.LoadOrCreateActive(1)` —
   safe to call per-append because it only creates a key on the very first
   call; every later call is a pure read (Phase 3's own tests already cover
   this).
4. Build the `Receipt`, compute `EntryHash` via the existing
   `receipt.ComputeEntryHash` (Phase 2 — also runs `Validate`), sign it with
   `signer.Sign`.
5. Insert the row. Commit. Any failure at any step rolls back and consumes
   no sequence (LEDG-07).

## Migration `002_receipts.sql`

Coexists with Phase 3's `002_signer_keys.sql` (both `002_`-prefixed,
independent, applied in alphabetical order by `RunMigrations` — established
precedent from Phase 3). Schema follows ARCHITECTURE.md's `receipts` table
exactly, with `signer_kid` referencing `signer_keys.kid` instead of a
`receipt_keys` table. Adds `UPDATE`/`DELETE`-rejecting triggers per
ARCHITECTURE.md's "not the tamper-evidence mechanism, but prevents
accidents" guidance. Does not alter `audit_log`, `tokens`, or `agent_keys`.

## Gateway request flow (`internal/gateway/gateway.go`)

Adopt ARCHITECTURE.md's `prepareAttempt`/`executeAttempt` split verbatim:
unauthenticated or malformed requests never call `prepareAttempt`'s
downstream steps and are never receipted (matches Phase 2's "Coverage
Boundary" — narrows the PRD's "every /v1/act call" to "every authenticated
action attempt", which is the more defensible product claim). Every other
exit — scope denial, rate limit, unknown service/action, missing/expired
token, upstream network failure, upstream HTTP response — builds an
`ActionOutcome`, appends its receipt, and only then writes the HTTP
response. A receipt append failure returns `500` and never returns the
would-be success response (LEDG-07).

`gateway.Config` gains three new fields using the narrow interfaces
ARCHITECTURE.md proposes (`ReceiptRecorder`, `AgentAuthorizer`,
`RequestLimiter`), so unit tests can use small fakes without opening SQLite.
The existing `AgentKeys map[string]string` MVP auth is replaced by
`Authorizer AgentAuthorizer` backed by `auth.KeyStore` — this is the
LEDG-03 requirement (verified key scopes before vault access).

Biscuit/delegation authorization is not wired — Phase 8 owns it. The
`DelegationChain` receipt field stays empty for every Phase 4 receipt.

## Composition root (`cmd/agentgw/main.go`)

Becomes the first real composition root: opens SQLite via `internal/db`,
runs migrations, constructs `vault.NewSQLiteStore`, `auth.NewKeyStore`,
`signer.NewStore` + one `LoadOrCreateActive(1)` call at boot, `ratelimit.New`,
`receipt.NewLedger`, and mounts `signer.PubkeyHandler` at
`GET /v1/receipts/pubkey` (Phase 3 built the handler; Phase 4 mounts it, as
Phase 3's CONTEXT.md deferred). Fixes the `AGENTGATE_VAULT_KEY` vs
`VAULT_ENCRYPTION_KEY` env var mismatch PROJECT.md and ARCHITECTURE.md both
flag — standardizes on `AGENTGATE_VAULT_KEY` (matches `.env.example` and
`docker-compose.yaml`).

The legacy `internal/audit` buffered logger is left in place, unused, per
ARCHITECTURE.md's explicit "preserve for compatibility, do not extend"
guidance — it is not deleted (no roadmap requirement asks for its removal)
and not wired into the new path.

## Crash and completeness limits (LEDG-11)

Documented directly in `ledger.go`'s package/type doc comment, matching
ARCHITECTURE.md's Failure Behavior section verbatim: no atomic transaction
spans SQLite and an external SaaS side effect; a SaaS action can complete
before a later receipt append fails. The milestone guarantees only (a) no
successful HTTP outcome before its receipt commits, and (b) committed
sequences have no allocation gaps — not exactly-once evidence under disk
failure.

## p99 latency (LEDG-10)

A Go benchmark (`BenchmarkLedgerAppend`) measures single-writer append
latency on a temp-file SQLite database. The measured value is recorded in
`04-01-SUMMARY.md`. Per PROJECT.md's constraint, the design is revisited
only if p99 exceeds 50ms — not expected for single-row inserts on local
SQLite with WAL mode.

</decisions>

<code_context>
## Existing Code Insights

- `internal/gateway/gateway.go`'s `handleAct` currently never calls
  `internal/audit`'s logger at all (confirmed by research and by direct
  read) — it only emits a structured `slog` line. The PRD's "replace
  internal/audit/logger.go usage" assumption is stale; there is no usage to
  replace, only a gap to fill.
- `internal/ratelimit.Limiter.Allow(agentKeyID, service string) bool` already
  matches the `RequestLimiter` interface ARCHITECTURE.md proposes exactly —
  no changes needed to that package.
- `auth.KeyStore.Validate(ctx, plaintext) (*AgentKey, error)` returns
  `ErrKeyNotFound`/`ErrKeyRevoked`/the key with `AllowedServices`/
  `AllowedUsers` already populated — `CanAccessService`/`CanAccessUser` are
  ready to call directly after `Validate` succeeds.
- `tests/integration/gateway_test.go`'s `setupIntegration` builds its own
  inline schema and constructs `audit.NewLogger` but never passes it to
  `gateway.New` — dead setup that Phase 4 replaces with a real ledger and
  receipt assertions, per ARCHITECTURE.md's explicit finding.
- `internal/db.RunMigrations` exec's every embedded file with no per-file
  transaction or migration ledger. ARCHITECTURE.md recommends adding
  `schema_migrations` tracking; that is a `internal/db` package
  improvement outside Phase 4's requirement list (no LEDG-0X asks for it)
  — deferred, noted as a residual risk in verification rather than
  implemented here, to keep this already-large phase bounded.

</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above and the committed research documents.

</specifics>

<deferred>
## Deferred Ideas

- `schema_migrations` tracking table and per-file transactional migrations:
  real improvement, but no LEDG-0X requirement asks for it. Left for a
  future infra-hardening pass, not blocking this milestone.
- Deleting `internal/audit`: no requirement asks for removal.
- Biscuit delegation wiring: Phase 8.
- `agentgate-verify` CLI: Phase 5.

</deferred>
