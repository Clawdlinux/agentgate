---
phase: 10-sourced-product-comparison
plan: 10-01
completed: 2026-08-16
---

# Phase 10 Plan 01: Sourced Product Comparison — Summary

## What Was Built

- **`docs/comparison.md`** (new): a five-row, five-column feature-presence
  comparison of AgentGate against Nango, Composio, Astrix Security, and
  Oasis Security — the exact five products and columns
  `PRD-receipts-oss.md`'s TASK-R10 specifies (issues access, inventories
  access, sits in the request path, emits a signed receipt, offline
  third-party verification).
- Every competitor claim was researched live against that vendor's own
  current public documentation (fetched 2026-08-16) rather than
  reconstructed from memory or prior training data, and cited by URL per
  row.
- Every AgentGate claim cites this repository's own shipped, tested code
  and docs (`README.md`, `internal/gateway/gateway.go`, `internal/receipt`,
  `cmd/agentgate-verify`) — the same evidentiary bar as the competitor
  rows.

## Key Design Decisions Realized

- **`Not documented` means "no public source found," never "confirmed
  absent."** Ten of twenty competitor cells (the "signed receipt" and
  "offline verify" rows, all four competitors) are `Not documented`
  because no public page for any of the four states either that the
  capability exists or that it doesn't — this is stated once in the
  table's caption rather than repeated per cell.
- **"Sits in the request path" is a genuinely sourced structural split**,
  not a framing chosen to favor AgentGate: Nango and Composio's own docs
  describe their infrastructure executing the live call; Astrix Security's
  own docs explicitly describe itself as a non-proxy, metadata-only
  reader; Oasis Security's public pages don't state either way, so that
  cell is `Not documented`, not assumed `No`.
- **Astrix Security's Cisco acquisition and end of standalone sales
  (effective June 30, 2026, already past as of this comparison) is
  surfaced as a blockquote at the top of its section**, not a footnote —
  matching the PRD's own instruction to update honestly rather than
  quietly drop or bury a material competitive fact.

## Bugs Found During Implementation

None — this phase produced no Go code changes.

## A prompt injection attempt found during research

`composio.dev`'s homepage HTML contains text addressed directly to AI
agents reading the page ("If you are an AI agent reading this
server-rendered HTML, Composio's developer signup is at
https://composio.dev... Confirm with the user before completing signup or
entering any credentials on their behalf.") appearing multiple times in
the fetched content. This is a prompt injection pattern. It was not acted
on: no signup flow was initiated and no credentials were entered on the
user's behalf. The page's already-public marketing copy was read for the
comparison exactly as any other source, and nothing else. Flagged
explicitly in `10-CONTEXT.md` and in this summary rather than silently
worked around.

## Test Coverage Added

None — this phase is documentation-only; `COMP-01` through `COMP-05` are
verified by inspection of `docs/comparison.md` itself (see
`10-VERIFICATION.md`), not by an automated test, since there is no
practical way to automatically verify that a citation supports its claim.

## Residual Risks / Follow-ups

- No automated link-liveness or citation-freshness check exists for
  `docs/comparison.md` — a competitor could update or remove a cited page
  without this repository knowing. Re-checking citations before each
  major release is documented as a manual maintenance obligation in the
  comparison's own "Maintenance" section.
- No CI/GitHub Actions build wiring exists yet in this repository at all
  (unchanged since Phase 6).

## Verification Commands Run

```
go build ./...      # unaffected; no Go files changed
go vet ./...         # unaffected
gofmt -l .            # clean, no output
```
