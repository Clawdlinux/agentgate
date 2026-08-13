---
phase: 05-independent-offline-verification
verified: 2026-08-14T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 5: Independent Offline Verification Verification Report

**Phase Goal:** Auditors can verify supplied receipt chains offline without signer secrets or gateway state.
**Verified:** 2026-08-14
**Status:** `passed`
**Re-verification:** No. This is the initial verification.

## Verdict

`agentgate-verify` reads SQLite or JSONL receipt chains, requires only a
locally saved trust file (no network, no gateway process, no private
key), and detects every required tamper case with the documented exit
code. All 5 roadmap success criteria and all 11 VER requirements have
direct, passing test coverage at both the pure-function level
(`internal/receipt/verifier_test.go`) and the real CLI level
(`cmd/agentgate-verify/main_test.go`, using genuinely signed receipts from
a real `Ledger`).

## Goal Achievement

### Observable Truths

| # | Roadmap truth | Status | Evidence |
|---|---|---|---|
| 1 | The verifier reads SQLite, JSONL files, and JSONL standard input without private keys, gateway state, or network access | VERIFIED | `readSQLite`/`readJSONL` in `main.go` only query/read; `TestRun_SQLite_ValidChainExitsZero`, `TestRun_JSONL_ValidChainExitsZero`; no import of `internal/gateway`, `internal/vault`, or any network client anywhere in `internal/receipt/verifier.go` or `cmd/agentgate-verify` |
| 2 | Verification requires a separately trusted public root and validates historical key transitions | VERIFIED | `LoadTrustedKeys` requires an explicit trust file; `TestVerifyChain_RotatedKeyBindsToItsValidityInterval` proves a receipt signed outside its resolved key's `[ValidFromSeq, ValidUntilSeq]` interval fails even with a mathematically valid signature (this milestone's documented trust-model substitute for a cryptographic transition chain — see `05-CONTEXT.md`) |
| 3 | Exit codes distinguish success, mismatches, and input failures while reporting the first failing sequence safely | VERIFIED | `TestRun_MissingFlagsExitTwo`, `TestRun_EmptySourceExitsTwo`, `TestRun_JSONL_MalformedLineExitsTwo` (exit 2); every `TestRun_SQLite_*ExitsOne` test (exit 1); `VerifyResult.Reason` uses only the fixed `Reason*` constants, never a receipt field (VER-10) |
| 4 | Separate tests return mismatch status for modified, interior-deleted, inserted, and forged receipt records | VERIFIED | `TestRun_SQLite_ModifiedRowExitsOne`, `TestRun_SQLite_DeletedRowExitsOne`, `TestRun_SQLite_InsertedRowExitsOne`, `TestRun_SQLite_ForgedSignatureExitsOne` — each a dedicated test, each against a real SQLite database with the append-only triggers dropped first to prove cryptographic detection, not the trigger, catches it |
| 5 | Malformed inputs fail as input errors, and raw-chain completeness requires a trusted expected head | VERIFIED | `TestRun_JSONL_MalformedLineExitsTwo`, `TestJSONL_RejectsUnsupportedFormatVersion` (exit 2); `TestVerifyChain_ExpectedHeadProvesCompleteness` / `TestRun_ExpectedHead` (completeness claimed only with a matching `--expected-head`); `TestVerifyChain_ExpectedHeadMismatchDetectsTruncation` (a truncated-but-internally-valid chain fails against a mismatched expected head) |

**Score:** 5/5 roadmap truths verified.

## Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| VER-01 | SATISFIED | `readSQLite`/`readJSONL` (file and `-` for stdin) in `cmd/agentgate-verify/main.go` |
| VER-02 | SATISFIED | `verifier.go` imports no HTTP/network/gateway package; `readSQLite` opens the file directly, no gateway process involved |
| VER-03 | SATISFIED | `TrustedKey`'s validity-interval binding; `TestVerifyChain_RotatedKeyBindsToItsValidityInterval`; trust model deviation documented in `05-CONTEXT.md` |
| VER-04 | SATISFIED | `run()`'s three `return` paths: 0 (`result.OK`), 1 (`!result.OK`), 2 (every other error path) |
| VER-05 | SATISFIED | `TestVerifyChain_ModifiedReceiptFails` (unit) + `TestRun_SQLite_ModifiedRowExitsOne` (CLI, exit 1) |
| VER-06 | SATISFIED | `TestVerifyChain_InteriorDeletedReceiptFails` (unit) + `TestRun_SQLite_DeletedRowExitsOne` (CLI, exit 1) |
| VER-07 | SATISFIED | `TestVerifyChain_InsertedReceiptFails` (unit) + `TestRun_SQLite_InsertedRowExitsOne` (CLI, exit 1) |
| VER-08 | SATISFIED | `TestVerifyChain_ForgedSignatureFails` (unit) + `TestRun_SQLite_ForgedSignatureExitsOne` (CLI, exit 1) |
| VER-09 | SATISFIED | `TestVerifyChain_EmptyInputIsConfigError`, `TestVerifyChain_NoTrustedKeysIsConfigError`, `TestVerifyChain_UnknownSignerKIDIsConfigError`, `TestJSONL_RejectsUnsupportedFormatVersion`, `TestRun_JSONL_MalformedLineExitsTwo`, `TestRun_EmptySourceExitsTwo` — all exit 2 |
| VER-10 | SATISFIED | `VerifyResult.Reason` is always one of the `Reason*` string constants; none reference `HumanPrincipal`, `AgentKeyID`, or any other receipted field |
| VER-11 | SATISFIED | `TestVerifyChain_ExpectedHeadProvesCompleteness`, `TestVerifyChain_ExpectedHeadMismatchDetectsTruncation`, `TestRun_ExpectedHead` |

No Phase 5 requirement is orphaned. REQUIREMENTS.md maps VER-01 through VER-11 to Phase 5 and R5.

## Scope Verification

New files: `internal/receipt/{verifier,verifier_test,jsonl,jsonl_test}.go`,
`cmd/agentgate-verify/{main,main_test}.go`,
`.planning/phases/05-independent-offline-verification/{05-CONTEXT,05-01-SUMMARY,05-VERIFICATION}.md`.

Modified: `Makefile` (`build-verify` target), `.planning/{ROADMAP,REQUIREMENTS,PROJECT,STATE}.md`.

No file under `internal/gateway`, `cmd/agentgw`, `internal/vault`, or
`internal/auth` changed. The signed bounded export (`GET
/v1/receipts/export`, Phase 7) and Biscuit delegation (Phase 8) are
untouched.

## Behavioral Checks

| Check | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l internal/receipt cmd/agentgate-verify` | clean |
| `gofmt -l .` (repo-wide) | only pre-existing `internal/registry/registry.go` (unchanged since Phase 3/4) |
| `go test ./internal/receipt/... -race -count=1` | PASS, includes 12 new verifier tests + 2 new JSONL tests |
| `go test ./cmd/agentgate-verify/... -race -count=1` | PASS, 10/10 |
| `go test ./... -race -count=1` | PASS across every package |
| `make build-verify` | builds `bin/agentgate-verify`; smoke-tested exit code 2 on an empty trust file |

## Residual Risks

- `internal/registry/registry.go` gofmt drift — pre-existing, out of
  scope, unchanged since Phase 3.
- Admin/OAuth HTTP routes still unmounted in the production binary — out
  of every phase's requirement list so far.
- `internal/db`'s missing per-file migration transaction/ledger — still
  deferred, still not required.
- `agentgate-verify` is not yet part of any release/CI packaging step
  beyond the new `make build-verify` target; CI wiring is Phase 6's
  quickstart concern.
