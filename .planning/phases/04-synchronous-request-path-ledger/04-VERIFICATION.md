---
phase: 04-synchronous-request-path-ledger
verified: 2026-08-14T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 4: Synchronous Request-Path Ledger Verification Report

**Phase Goal:** Authenticated action attempts produce gap-free committed receipts before AgentGate returns a successful response.
**Verified:** 2026-08-14
**Status:** `passed`
**Re-verification:** No. This is the initial verification.

## Verdict

The request path now commits one signed, chained receipt for every
authenticated, schema-valid `/v1/act` attempt before writing any HTTP
response. All 5 roadmap success criteria and all 11 LEDG requirements have
direct, passing test coverage — unit-level in `internal/receipt` and
`internal/gateway`, and end-to-end in `tests/integration` through the real
HTTP path with a real, file-backed SQLite database.

## Goal Achievement

### Observable Truths

| # | Roadmap truth | Status | Evidence |
|---|---|---|---|
| 1 | A populated database upgrades through `002_receipts.sql` without changing `audit_log` data | VERIFIED | `002_receipts.sql` only creates `receipts`, its indexes, and its triggers; `TestRunMigrations_AppliesAllMigrationsCleanly` (Phase 3, still passing) confirms all 4 pre-existing tables plus `receipts` exist after migration |
| 2 | Production uses persistent SQLite auth, vault, migrations, scopes, and receipt dependencies before vault access | VERIFIED | `cmd/agentgw/main.go` opens SQLite, runs migrations, constructs `vault.NewSQLiteStore`/`auth.KeyStore`/`signer.Store`/`receipt.Ledger`; `executeAttempt` checks `CanAccessService`/`CanAccessUser` before any `Vault.Get` call |
| 3 | Every authenticated, schema-valid attempt reaches one receipt path for allowed, denied, limited, token, and upstream outcomes | VERIFIED | `TestAct_ScopeDenied` (deny), `TestAct_RateLimited` (rate_limited), `TestAct_NoToken` (allow/token_missing), `TestAct_Success` (allow); `TestAct_Unauthorized`/`TestAct_MissingFields` confirm the coverage boundary excludes unauthenticated/malformed requests |
| 4 | Concurrent and restarted actions preserve committed sequence order without gaps, duplicates, or an in-memory source of truth | VERIFIED | `TestLedgerAppend_HundredConcurrentAppendsProduceExactSequence1To100` (100 goroutines, `-race`); `TestLedgerAppend_RestartResumesFromCommittedHead`; `TestIntegration_ConcurrentActsProduceGapFreeChain` (25 real concurrent HTTP requests) |
| 5 | Failed receipt commits consume no sequence or success response. Measured p99 and docs exclude atomic SQLite plus SaaS guarantees | VERIFIED | `TestLedgerAppend_InvalidDraftConsumesNoSequence`; `TestAct_ReceiptAppendFailure` (gateway returns 500, never the outcome); `BenchmarkLedgerAppend` = ~122µs/op (400x under the 50ms threshold); crash/completeness limits documented in `ledger.go`'s doc comment |

**Score:** 5/5 roadmap truths verified.

## Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| LEDG-01 | SATISFIED | `002_receipts.sql` is additive only; `audit_log` untouched |
| LEDG-02 | SATISFIED | `cmd/agentgw/main.go` composition root: `db.Open`+`RunMigrations`, `vault.NewSQLiteStore`, `auth.NewKeyStore`, `signer.NewStore`, `receipt.NewLedger` |
| LEDG-03 | SATISFIED | `executeAttempt` calls `CanAccessService`/`CanAccessUser` on the `auth.KeyStore`-verified key before `Vault.Get` |
| LEDG-04 | SATISFIED | `prepareAttempt`/`executeAttempt` split: every request past authentication and shape validation reaches `handleAct`'s single `s.cfg.Receipts.Append` call |
| LEDG-05 | SATISFIED | `TestAct_ScopeDenied` (deny), `TestAct_RateLimited` (rate_limited), `TestAct_NoToken` (token_missing), `TestAct_Success` (allow); upstream-failed path uses the same `outcome`/receipt shape (`errorCode: "upstream_error"`) |
| LEDG-06 | SATISFIED | `Ledger.Append`'s single `BEGIN IMMEDIATE` transaction: head read, signer key fetch, hash, sign, insert, commit |
| LEDG-07 | SATISFIED | `TestLedgerAppend_InvalidDraftConsumesNoSequence`; `TestAct_ReceiptAppendFailure` |
| LEDG-08 | SATISFIED | `Ledger` holds no head/sequence field; `Head`/`Append` always query SQLite; `TestLedgerAppend_RestartResumesFromCommittedHead` |
| LEDG-09 | SATISFIED | `TestLedgerAppend_HundredConcurrentAppendsProduceExactSequence1To100` (ledger-level, 100 goroutines) and `TestIntegration_ConcurrentActsProduceGapFreeChain` (HTTP-level, 25 goroutines) |
| LEDG-10 | SATISFIED | `BenchmarkLedgerAppend` measured and recorded in `04-01-SUMMARY.md`: ~122µs/op, well under 50ms |
| LEDG-11 | SATISFIED | `Ledger`'s doc comment states the no-atomic-SQLite-plus-SaaS-transaction limit and the two guarantees actually provided |

No Phase 4 requirement is orphaned. REQUIREMENTS.md maps LEDG-01 through LEDG-11 to Phase 4 and R4.

## Scope Verification

New files: `internal/receipt/{ledger,ledger_test}.go`,
`internal/db/migrations/002_receipts.sql`, `internal/auth/keys_test.go`,
`.planning/phases/04-synchronous-request-path-ledger/{04-CONTEXT,04-01-SUMMARY,04-VERIFICATION}.md`.

Modified files: `internal/gateway/gateway.go` (full request-path rewrite),
`internal/gateway/gateway_test.go` (rewritten against the new `Config`
shape), `internal/auth/keys.go` (`Count` added), `internal/db/sqlite.go`
(`_foreign_keys=on`), `cmd/agentgw/main.go` (composition root rewrite),
`tests/integration/gateway_test.go` (rewritten against real dependencies),
`.env.example`, `docker-compose.yaml`, `Dockerfile` (env var and default
`--db` path fixes), `.planning/{ROADMAP,REQUIREMENTS,PROJECT,STATE}.md`.

Biscuit delegation wiring, the `agentgate-verify` CLI, and the export
endpoint are untouched — they are Phase 8, Phase 5, and Phase 7 scope
respectively, per ROADMAP.md.

## Behavioral Checks

| Check | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l` (Phase 4 files) | clean |
| `gofmt -l .` (repo-wide) | only pre-existing `internal/registry/registry.go` (documented since Phase 3, out of scope) |
| `go test ./internal/receipt/... -race -count=1` | PASS, 15/15 (6 new ledger tests + Phase 2's existing receipt-protocol tests) |
| `go test ./internal/gateway/... -race -count=1` | PASS, 12/12 |
| `go test ./internal/auth/... -race -count=1` | PASS (2 new `Count` tests) |
| `go test ./tests/integration/... -race -count=1` | PASS, 8/8 (including the new receipt-row assertion and 25-concurrent-request chain test) |
| `go test ./... -race -count=1` | PASS across every package |
| `BenchmarkLedgerAppend` | ~122µs/op, single writer, file-backed SQLite |

## Residual Risks

- `internal/registry/registry.go` gofmt drift — pre-existing, out of scope, unchanged since Phase 3's note.
- Admin/OAuth HTTP routes remain unmounted in the production binary; the
  bootstrap agent key is the only way to obtain a first credential without
  direct database access. No LEDG-0X requirement covers this; flagged for
  a future phase.
- `internal/db.RunMigrations` still has no per-file transaction or
  migration ledger (ARCHITECTURE.md's recommendation) — real improvement,
  not required by any LEDG-0X item, deferred.
