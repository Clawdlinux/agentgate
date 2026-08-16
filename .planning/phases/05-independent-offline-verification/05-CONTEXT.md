---
phase: 05-independent-offline-verification
gathered: 2026-08-14
status: ready-for-planning
mode: autonomous (autopilot loop, no interactive user available)
research: .planning/research/ARCHITECTURE.md ("Offline Verification",
  "Verifier Trust Model" sections; already committed, read directly rather
  than re-derived), PRD-receipts-oss.md TASK-R5,
  agentic-operator-core/cmd/audit-verify/main.go (structural analog)
---

<domain>
## Phase Boundary

Ship an offline verifier for receipt chains: VER-01 through VER-11. No
gateway state, private key, or network access may be required to verify.
The signed bounded export (`GET /v1/receipts/export`, manifest-anchored
partial ranges) is Phase 7 — this phase verifies whole local ledgers
(SQLite table or a JSONL dump of one), not bounded/manifest slices.

</domain>

<decisions>
## Trust model reconciliation (the one real deviation)

The committed `ARCHITECTURE.md` (written before Phase 3 shipped) describes
a verifier that starts from **one** pinned root public key and
cryptographically walks a signed rotation-transition chain to trust later
keys. Phase 3 shipped and verified a narrower design — `internal/signer` +
`signer_keys` — that satisfies every KEY-0X requirement without signing a
transition payload between rotated keys (recorded in `03-CONTEXT.md` and
`04-CONTEXT.md`; not reopened here for the same reason: no requirement
asks for it, and reopening a verified phase for an unrequired feature is
scope creep).

VER-03 says the verifier "validates historical key transitions." Given the
shipped schema, this phase satisfies that requirement differently: the
auditor's trust file must contain **every** key that ever signed a receipt
in the checked range (the full history `GET /v1/receipts/pubkey` returns),
obtained once through a trusted channel. `VerifyChain` binds each
receipt's `signer_kid` to that key's declared
`[ValidFromSeq, ValidUntilSeq]` sequence interval (inclusive — confirmed
against `signer.Store.Rotate`'s `untilSeq := atSeq - 1` semantics) —
a receipt signed by a key outside its own validity window fails
verification. This is a real, defensible check (it catches an attacker
claiming a key was still active after rotation, or before its first use),
just not a cryptographic chain-of-custody between keys. Documented in
`TrustedKey`'s doc comment in `verifier.go`.

## Exit code mapping (VER-04, VER-09)

Two failure classes map to two different exit codes, matching the
distinction `VerifyChain`'s signature makes explicit (`(VerifyResult,
error)`):

- **Exit 2 (input/config error)**: empty receipt source, empty trust file,
  unsupported `format_version`, malformed JSONL/SQLite rows, and —
  deliberately — an **unknown `signer_kid`**. VER-09 groups "unknown key
  IDs" with the other input-error cases; a verifier holding an incomplete
  or stale trust file cannot distinguish "attacker's key" from "my trust
  file is out of date," so this is treated as a configuration problem, not
  a tamper finding.
- **Exit 1 (tamper finding)**: sequence gaps, `prev_hash` mismatches,
  `entry_hash` mismatches, invalid Ed25519 signatures, a known key used
  outside its validity interval, and an `--expected-head` mismatch.

## Genesis-anchored verification, not bounded ranges (VER-11)

`VerifyChain` always requires the first receipt to be `Seq == 1` with a
zero `PrevHash` — it verifies a whole local ledger or a full JSONL dump of
one, not an arbitrary bounded slice. `--expected-head SEQ:HEXHASH` adds a
second, independent claim: the *tail* matches a separately trusted value.
This distinguishes two failure modes an internally consistent chain cannot
tell apart on its own: a missing prefix/interior row (caught by the
genesis+sequence checks) versus a missing suffix (undetectable without an
externally known expected head — PITFALLS.md's "a valid local chain does
not prove completeness"). Bounded, manifest-anchored partial-range
verification (starting mid-chain from a signed export manifest) is Phase
7's `GET /v1/receipts/export` scope, not this phase's.

## `format_version` lives at the storage/wire layer, not on `Receipt`

Phase 2's `receipt.Receipt` struct has no `FormatVersion` field — the
protocol itself doesn't carry a version tag. `002_receipts.sql` (Phase 4)
does have a `format_version` column, and the JSONL encoding
(`internal/receipt/jsonl.go`, new this phase) has a `format_version` JSON
field. Both adapters reject any value other than `1` before constructing a
`Receipt`, keeping version-checking a concern of the two storage adapters
rather than the pure protocol type.

## Package layout

- `internal/receipt/verifier.go`: `TrustedKey`, `LoadTrustedKeys`,
  `ExpectedHead`, `ParseExpectedHead`, `VerifyResult`, `VerifyChain` — all
  pure, no database or HTTP imports (research's explicit guidance).
- `internal/receipt/jsonl.go`: `MarshalJSONLReceipt`/`ParseJSONLReceipt` —
  shared with Phase 7's future export feature, not verifier-only.
- `cmd/agentgate-verify/main.go`: CLI only. `run(args, stdout, stderr) int`
  is the testable core (mirrors
  `agentic-operator-core/cmd/audit-verify/main.go`'s `run() int` shape);
  `main()` is a two-line `os.Exit(run(...))` wrapper. SQLite and JSONL
  input adapters live here, not in the receipt package, matching the
  reference analog's `readJSONL`/`readClickHouse` placement.

</decisions>

<code_context>
## Existing Code Insights

- `signer.Store.Rotate(atSeq)` sets the retiring key's `ValidUntilSeq =
  atSeq - 1` — **inclusive** of that sequence, not an exclusive upper
  bound. Got this wrong on the first pass (`r.Seq >= *ValidUntilSeq`,
  which incorrectly rejected the last receipt the old key legitimately
  signed) — caught immediately by
  `TestVerifyChain_RotatedKeyBindsToItsValidityInterval`, fixed to `r.Seq
  > *ValidUntilSeq`.
- `002_receipts.sql`'s `trg_receipts_no_update`/`trg_receipts_no_delete`
  triggers (Phase 4) block direct SQL tampering — a real, useful defense,
  but not the tamper-*evidence* mechanism. The CLI-level tamper tests
  explicitly drop these triggers before mutating a row, simulating an
  attacker with enough SQLite access to defeat them, to prove
  cryptographic verification (not the trigger) is what actually detects
  the tamper.
- `agentic-operator-core/pkg/audit/verifier.go`'s `Report`/`Walk` shape
  (`TotalEntries`, `OK`, `FirstError`, `FirstErrorSeq`, head tracking) is
  the direct structural analog for `VerifyResult`/`VerifyChain` — adapted
  from HMAC-keyed single-hasher lookup to an Ed25519 `TrustedKey` map with
  a validity-interval check.

</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.

</specifics>

<deferred>
## Deferred Ideas

- Bounded/manifest-anchored export verification (`GET
  /v1/receipts/export`, signed manifest lines): Phase 7.
- A cryptographic key-rotation transition chain: not required by any
  KEY-0X or VER-0X requirement; would only strengthen an already-passing
  VER-03 interpretation.
- `internal/db`'s missing `schema_migrations` ledger: still deferred from
  Phase 4, still not required by anything in this phase.

</deferred>
