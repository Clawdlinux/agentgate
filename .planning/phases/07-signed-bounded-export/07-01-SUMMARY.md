---
phase: 07-signed-bounded-export
plan: 07-01
completed: 2026-08-16
---

# Phase 7 Plan 01: Signed Bounded Export — Summary

## What Was Built

- **`VerifyChain` gained a required `Anchor` parameter** (`internal/receipt/verifier.go`).
  `Anchor{Seq, EntryHash}` is the committed state immediately before the
  first receipt being verified. `Anchor{}` (the zero value) reproduces
  Phase 5's exact genesis-only behavior; every existing call site
  (`verifier_test.go`, `cmd/agentgate-verify`) was updated to pass it
  explicitly. This is the change that makes bounded, mid-chain
  verification possible at all — Phase 5 hard-required `Seq == 1`.
- **`internal/receipt/export.go`** (new): the signed manifest type and
  typed-JSONL-line encoding.
  - `ExportManifest` binds format version, requested/resolved bounds,
    count, anchor, first/last exported entry hashes, the database's true
    head at snapshot time, a keyset digest, and a signer KID + signature.
  - `ComputeKeysetDigest` hashes a canonical, `kid`-sorted encoding of the
    embedded trusted keys, so the manifest's signature transitively covers
    the exact trust set shipped alongside it.
  - `SignManifest`/`VerifyManifest` reuse `internal/signer` exactly as
    `Ledger.Append` does — no new key material.
  - `MarshalManifestLine`/`ParseManifestLine`, `MarshalKeyLine`/
    `ParseKeyLine`, and `DetectJSONLLineType` implement the typed-line
    JSONL format (`"type": "manifest" | "key" | "receipt"`). A line with
    no `"type"` field defaults to `"receipt"`, so old Phase 5 exports still
    parse unchanged.
- **`internal/receipt/jsonl.go`**: `MarshalJSONLReceipt` now emits
  `"type": "receipt"` explicitly (backward compatible either way).
- **`internal/receipt/export_handler.go`** (new): `GET /v1/receipts/export?
  from=&to=`.
  - Reads the anchor row, the requested receipt range, every signer key,
    and the true head inside one `BeginTx(ctx, &sql.TxOptions{ReadOnly:
    true})` — a single consistent snapshot, matching the architecture
    note that nothing may commit between reading the head and reading the
    keyset.
  - `resolveExportRange` is a small pure function (not a method, takes no
    I/O) that clamps an absent or too-large `to` down to the true head and
    rejects `from > head+1` or a resolved range over `MaxExportRange`
    (10,000 receipts) with `400`. Splitting this out of the handler let it
    be unit tested directly without constructing 10,000+ real database
    rows.
  - `ScanReceiptRow` is a shared row-scan helper used by both the export
    handler and `cmd/agentgate-verify`'s `readSQLite`, removing a
    duplicated 25-line column-to-struct mapping that existed only in the
    CLI before this phase.
  - Writes `application/x-ndjson`: one manifest line, then key lines, then
    receipt lines, all signed and snapshot-consistent.
  - Mounted in `cmd/agentgw/main.go` behind `adminHandler.RequireAdmin`,
    alongside the existing `/admin/*` routes.
- **`cmd/agentgate-verify/main.go`** rewritten for typed-line support:
  - `--trust-root` is now optional — required only if the JSONL input has
    no embedded keys.
  - `readJSONL` now returns `(receipts, embeddedKeys, manifest, err)`,
    dispatching on `DetectJSONLLineType` per line.
  - When a manifest is present, `run()` verifies it first (`VerifyManifest`,
    checking both the signature and the keyset digest), derives the
    `Anchor` and (absent an explicit `--expected-head`) the `ExpectedHead`
    from the manifest, then calls `VerifyChain` exactly as before.
  - On success, prints `range: full` or `range: partial (head at export
    time was seq=N)` based on whether the manifest's resolved bound
    reached its own reported head.

## Key Design Decisions Realized

- **Anchor is a real, deliberate API break, not an additive option.**
  Every caller must now state what came before the receipts it's checking.
  This was chosen over an "optional pointer, nil means genesis" design
  because a nil-means-something-implicit default is exactly the kind of
  ambiguity that has caused silent-acceptance bugs in verifier code before
  (see Phase 5's own `ValidUntilSeq` inclusive/exclusive bug, found during
  this same milestone). A zero-value struct communicates the same intent
  more explicitly at every call site.
- **Trust precedence is simple, not "smart."** Explicit `--trust-root`
  always wins if given; otherwise fall back to the export's own embedded
  keys; error if neither is present. A "cross-check both automatically"
  design was considered and rejected as unnecessary complexity — no
  requirement asks for it, and an operator who wants that comparison can
  still supply `--trust-root` and get `VerifyManifest`'s keyset-digest
  check as the cross-check.
- **Range validation clamps `to`, but not `from`.** An absent or
  over-large `to` silently succeeds at the true head (transparently
  reported via `requested_to` vs `resolved_to` in the manifest) — this
  matches an auditor's likely intent ("give me everything you have") more
  than forcing a separate head-discovery round trip. `from` beyond
  `head+1` is always rejected, since there is no valid starting point to
  clamp to.

## Bugs Found During Implementation

- **Go's multi-value-return call-expression rule.** `writeJSONLLine(w,
  MarshalManifestLine(manifest))` does not compile: a function call with a
  multi-value return (`([]byte, error)`) cannot be combined with a
  sibling leading argument in the same call expression. Fixed by
  unpacking `(line, err)` into local variables before each call. Purely a
  syntax-level issue, not a logic bug — caught immediately by `go build`.
- **No new protocol bugs found this phase** (unlike Phase 5's
  `ValidUntilSeq` inclusive/exclusive bug) — the `Anchor` design was
  reviewed against the existing genesis special case in `07-CONTEXT.md`
  before implementation, and the zero-value equivalence was verified by
  keeping every existing Phase 5 test passing unmodified in behavior
  (only the call signature changed).

## Test Coverage Added

- `internal/receipt/export_test.go`: `ComputeKeysetDigest` order-
  independence and change-sensitivity; `SignManifest`/`VerifyManifest`
  round trip, forged-signature rejection, keyset-digest-mismatch
  rejection, unknown-signer-KID rejection; `MarshalManifestLine`/
  `ParseManifestLine` and `MarshalKeyLine`/`ParseKeyLine` round trips;
  `DetectJSONLLineType` default-to-receipt behavior.
- `internal/receipt/export_handler_test.go`: full-range export verifies
  offline end-to-end via `VerifyChain`; partial-range export uses the real
  mid-chain anchor hash and still verifies; `to` beyond head is clamped,
  not rejected; four distinct invalid-range 400 cases; a wiring sanity
  check that clamping does not accidentally trip the size cap; a
  boundary-value proof that the export never leaks a raw field value
  (using a value that would only ever survive verbatim if a raw parameter
  were mistakenly included, since `Ledger.Append` itself already rejects
  non-stable-code `Error` values).
- `resolveExportRange` unit tests exercise the 10,000-row size cap and the
  `from > head+1` / `from == head+1` boundary directly, without needing to
  construct oversized real database state.
- `internal/receipt/verifier_test.go`: two new tests prove `VerifyChain`
  correctly verifies a mid-chain slice given a real non-genesis `Anchor`,
  and correctly rejects a forged/mismatched anchor hash
  (`ReasonPrevHashMismatch`).
- `cmd/agentgate-verify/main_test.go`: three new end-to-end tests build a
  real chain via `Ledger`, serve it through `ExportHandler` over
  `httptest`, and feed the resulting JSONL straight into `run()` with **no**
  `--trust-root` flag — proving self-contained, embedded-key verification
  actually works for both a full-range and a partial-range export, and
  that a tampered manifest signature still fails closed (`exit 1`) even
  though every individual receipt line is untouched.

## Residual Risks / Follow-ups

- `internal/registry/registry.go`'s gofmt state remains clean as of this
  phase (confirmed, no regression) — Phase 6's cleanup holds.
- No CI/GitHub Actions build wiring yet — unchanged, not required by any
  EXPT-0X item.
- Rekor/Sigstore checkpoint mirroring and `POST /admin/receipts/keys/
  rotate` remain explicitly out of scope for this phase (see
  `07-CONTEXT.md`'s "What is deliberately not built").
- The manifest's completeness claim ("range: full") is only ever as
  trustworthy as the snapshot transaction it was read inside; it says
  nothing about receipts appended *after* export time, by design (an
  export is a point-in-time artifact, not a live subscription).

## Verification Commands Run

```
go build ./...
go vet ./...
gofmt -l .                          # clean, no output
go test ./... -race -count=1        # all packages pass
```
