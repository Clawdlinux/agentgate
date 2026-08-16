---
phase: 05-independent-offline-verification
plan: 05-01
completed: 2026-08-14
status: complete
---

# Phase 5 Summary: Independent Offline Verification

## What was built

- `internal/receipt/verifier.go`: `VerifyChain(receipts, trustedKeys,
  expectedHead) (VerifyResult, error)` — pure, no database or HTTP
  imports. Walks receipts in order, enforcing genesis anchoring (`Seq ==
  1`, zero `PrevHash` on the first row), sequence contiguity, `PrevHash`
  chaining, `entry_hash` recomputation, Ed25519 signature verification
  against a `TrustedKey` resolved by `signer_kid`, and — the milestone's
  chosen substitute for a cryptographic rotation-transition chain — that
  the resolved key's `[ValidFromSeq, ValidUntilSeq]` interval actually
  covers the receipt's sequence. An optional `ExpectedHead{Seq,
  EntryHash}` additionally proves the checked range is complete, not
  merely internally consistent (VER-11). `LoadTrustedKeys`/
  `ParseExpectedHead` parse the CLI's two remaining inputs.
- `internal/receipt/jsonl.go`: `MarshalJSONLReceipt`/`ParseJSONLReceipt` —
  one receipt per JSONL line, lowercase-hex fixed-length fields,
  `format_version` checked against exactly `1`.
- `cmd/agentgate-verify/main.go`: the CLI. `--source sqlite|jsonl --path
  <path|-> --trust-root <file> [--expected-head SEQ:HEXHASH]`. `run(args,
  stdout, stderr) int` is the testable core; `main()` just calls
  `os.Exit(run(...))`. Exit 0 pass, 1 tamper/mismatch, 2 input/config
  error — matching `agentic-operator-core/cmd/audit-verify/main.go`'s
  convention.
- Full test coverage: `internal/receipt/verifier_test.go` (12 tests
  exercising every VER requirement at the `VerifyChain` function level,
  using real signed receipts from a real `Ledger`+`signer.Store`, not hand
  crafted fixtures), `internal/receipt/jsonl_test.go` (round trip +
  version rejection), and `cmd/agentgate-verify/main_test.go` (10 tests
  calling `run()` directly against a real SQLite database and a real
  JSONL file, proving the actual exit codes 0/1/2 for every VER-05
  through VER-09 and VER-11 case, including tampering that first drops
  the append-only triggers to prove the *cryptographic* check — not the
  trigger — is what catches it).

## Key test results

- `go test ./internal/receipt/... -race -count=1`: PASS, all tests
  including the 12 new verifier tests and 2 new JSONL tests.
- `go test ./cmd/agentgate-verify/... -race -count=1`: PASS, all 10 CLI
  tests — modified row, deleted row, inserted/spliced foreign row, forged
  signature, malformed JSONL line, empty source, missing flags, and both
  directions of `--expected-head` all produce the documented exit code.
- `go build ./...`, `go vet ./...`, `gofmt -l internal/receipt
  cmd/agentgate-verify` all clean.
- Full suite: `go test ./... -race -count=1` — every package passes.
- Added a `build-verify` Makefile target so `agentgate-verify` builds the
  same way `agentgate` does (`make build-verify`).

## Deviations from the plan

- VER-03's "validates historical key transitions" is satisfied by
  validity-interval binding against a trust file containing every
  historical key, not a cryptographic transition chain — Phase 3 never
  produced transition signatures, and reopening that verified phase for
  an unrequired feature would be scope creep. Recorded in `05-CONTEXT.md`.
- Bounded, manifest-anchored partial-range verification is explicitly out
  of this phase (Phase 7's `GET /v1/receipts/export` scope). `VerifyChain`
  always requires genesis anchoring.

## Bug caught during implementation

First draft of the validity-interval check used `r.Seq >= *ValidUntilSeq`
(exclusive upper bound). `signer.Store.Rotate` actually sets
`ValidUntilSeq = atSeq - 1`, i.e. **inclusive** of that sequence — the
exclusive check incorrectly rejected the very last receipt the retiring
key legitimately signed. Caught immediately by
`TestVerifyChain_RotatedKeyBindsToItsValidityInterval`'s happy-path
assertion (not just its tamper assertion), fixed to `r.Seq >
*ValidUntilSeq`.

## Residual risks

- Same three noted in Phase 4's summary, unchanged: `internal/registry/registry.go`
  gofmt drift, admin/OAuth routes unmounted in the production binary, and
  `internal/db`'s missing migration ledger. None are in this phase's
  requirement list.
