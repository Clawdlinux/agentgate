---
phase: 07-signed-bounded-export
gathered: 2026-08-16
status: ready-for-planning
mode: autonomous (continuing established roadmap execution)
research: .planning/research/ARCHITECTURE.md ("Export Architecture",
  "Deletion Limits", "SQLite Snapshot Safety" sections; already committed,
  read directly), PRD-receipts-oss.md TASK-R6
---

<domain>
## Phase Boundary

EXPT-01 through EXPT-05: an admin exports an inclusive `[from, to]`
sequence range as one signed, snapshot-consistent JSONL artifact, which
verifies offline through the same `agentgate-verify` binary Phase 5
shipped — full or partial. Biscuit delegation (Phase 8) and Google
Workspace (Phase 9) are untouched.

</domain>

<decisions>
## `VerifyChain` must support a non-genesis anchor (a real API change)

Phase 5's `VerifyChain` hard-required the first receipt to be `Seq == 1`
with a zero `PrevHash` — deliberately, since Phase 5's scope was "whole
local ledgers," and its own `05-CONTEXT.md` explicitly deferred "bounded,
manifest-anchored partial-range verification" to this phase. A partial
export (`from=5, to=10`) has a first row with `Seq == 5`, which the old
check would always reject.

`VerifyChain` gains a new required parameter: `anchor Anchor`, where
`Anchor{Seq, EntryHash}` is the committed state immediately before the
first receipt being checked. The zero value `Anchor{}` (`Seq: 0,
EntryHash: zero`) represents genesis and reproduces Phase 5's exact
existing behavior — every current caller (the CLI, `verifier_test.go`)
updates to pass `Anchor{}` explicitly, so nothing already shipped changes
behavior; only the option to start elsewhere is added.

## Manifest-embedded trust, not a bigger `--trust-root`

The exported JSONL is self-contained: one manifest line, then key lines
(the `TrustedKey` records — `kid`, `public_key_hex`, validity interval —
covering every signer in the exported range), then receipt lines, each
with an explicit `"type"` field (`"manifest"`, `"key"`, `"receipt"`).
Old-format Phase 5 JSONL (plain receipt objects, no `"type"` field) still
parses exactly as before — a missing `"type"` defaults to `"receipt"`.
`agentgate-verify` reads embedded key lines as its trust set when present,
so verifying an export needs no separate `--trust-root` file; supplying
one anyway is still allowed for cross-checking a stale trust file against
the export's own signed keyset.

## The manifest is signed by the same Ed25519 signer as every receipt

The manifest binds: format version, requested `from`/`to`, resolved `to`
(clamped to the true head if the caller asked for more than exists),
receipt count, the anchor (`seq`/`hash` immediately before `from` — zero
hash for `from=1`), first and last exported entry hashes, the database's
true head `seq`/hash at snapshot time, a keyset digest (SHA-256 over the
canonical, `kid`-sorted encoding of every embedded key line), and a
`signer_kid`/signature pair over all of the above. Signing reuses
`signer.Store` exactly as `Ledger.Append` does — no new key material, no
new trust anchor.

## Completeness: two different questions, both answered

"Does this export match what it claims" (no rows removed/reordered/
recounted since the manifest was signed) is answered by recomputing the
same anchor + chain-walk + hash/signature checks `VerifyChain` already
does, now anchored at the manifest's own `anchor_seq`/`anchor_hash`, and
requiring the last exported row to match the manifest's own
`last_entry_hash` (this is just `ExpectedHead`, sourced from the manifest
instead of a CLI flag). "Does this export reach the *current* true head"
(full vs partial) is a separate, additional check: resolved `to` vs the
manifest's own reported head `seq`. An explicit `--expected-head` flag
still overrides the manifest-derived one, for an auditor who holds an
independently-obtained value they trust more than this specific export.

## Single-snapshot consistency

The HTTP handler reads the receipts range, the anchor row, the head row,
and every signer key inside one `BeginTx(ctx, &sql.TxOptions{ReadOnly:
true})` — not `Ledger`'s own methods (which use their own connections) —
so nothing can commit between reading the head and reading the keys.
Matches ARCHITECTURE.md's "read the manifest inputs and receipt range in
one SQLite snapshot transaction."

## Range validation

`from` is required and must be `>= 1`. `to` is optional; when absent, it
resolves to the current head. When `to` is present but exceeds the head,
it is clamped, not rejected — the manifest's own `requested_to` vs
`resolved_to` fields make this fully transparent to the caller (matches
"reports the first failing sequence... safely" spirit and avoids forcing
a client to poll the head separately just to ask for "everything"). A
fixed maximum range size (`10,000` receipts) rejects genuinely oversized
requests with `400` — matches "rejects invalid or oversized ranges."
`from > head+1` (nothing to export, and not even a valid starting point)
is also `400`.

## What is deliberately not built

Rekor/external-checkpoint mirroring stays out of scope (ARCHITECTURE.md's
explicit "Deletion Limits" note): a signed export manifest proves no
tampering *within* the exported range and gives a real anchor for
completeness, but detecting an unknown deleted *suffix* beyond any
manifest's own reported head still requires an externally trusted
checkpoint this milestone does not build. `POST /admin/receipts/keys/
rotate` (also listed in ARCHITECTURE.md's route table) is not part of
this phase's requirement list — no EXPT-0X item asks for a rotation
endpoint, only for export.

</decisions>

<code_context>
## Existing Code Insights

- `internal/receipt/verifier.go`'s `TrustedKey`, `LoadTrustedKeys`, and the
  `Reason*` constants are reused as-is; `ErrUnknownSignerKID`/
  `ErrNoTrustedKeys`/`ErrEmptyChain` semantics are unchanged.
- `internal/receipt/ledger.go`'s `readHead` helper (works over both
  `*sql.DB` and `*sql.Conn` via the `headReader` interface) is directly
  reusable inside the export handler's transaction for reading the true
  head.
- `cmd/agentgate-verify/main.go`'s `readJSONL`/`readSQLite` need the typed-
  line change; `readSQLite` gains a `from`/`to` — no, `readSQLite` stays
  whole-ledger only per VER-01 (SQLite reads are always the live table,
  not a bounded export); only the JSONL path deals with manifests.
- `internal/gateway/gateway.go`'s `maxRequestBodyBytes` pattern (a package
  constant, not a config field) is the precedent for the export range cap.

</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.

</specifics>

<deferred>
## Deferred Ideas

- `POST /admin/receipts/keys/rotate`: not required by any EXPT-0X item.
- Rekor/Sigstore checkpoint mirroring: explicitly out of scope per
  PROJECT.md's constraints, unchanged.
- Compressing or paginating very large exports: no requirement asks for
  it; the fixed range cap is the only guard this phase adds.

</deferred>
