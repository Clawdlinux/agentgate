---
phase: 10-sourced-product-comparison
verified: 2026-08-16T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 10: Sourced Product Comparison Verification Report

**Phase Goal:** Readers can compare documented product capabilities without unsupported negatives or market claims.
**Verified:** 2026-08-16
**Status:** `passed`
**Re-verification:** No. This is the initial verification.

## Verdict

`docs/comparison.md` was written from live research against each vendor's
own current public documentation (not reconstructed from memory), with
every claim — including every AgentGate claim — carrying a citation. A
material, dated fact about one competitor's ownership and sales status
(Astrix Security's Cisco acquisition and end of standalone sales) was
surfaced prominently rather than dropped. A prompt injection attempt
encountered during research (on `composio.dev`) was recognized and not
acted on.

## Goal Achievement

### Observable Truths

| # | Roadmap truth | Status | Evidence |
|---|---|---|---|
| 1 | `docs/comparison.md` contains the 5 required products and 5 required capability columns | VERIFIED | Table rows: Nango, Composio, Astrix Security, Oasis Security, AgentGate; columns: issues access, inventories access, sits in request path, emits signed receipt, offline third-party verify |
| 2 | Every positive competitor claim links to dated first-party public documentation | VERIFIED | Every "Yes"/"No" cell for every competitor cites a specific vendor URL with a fetch date (2026-08-16) in the "Sourcing, row by row" section |
| 3 | Missing public evidence appears as `Not documented`, never as a definitive absence | VERIFIED | 10 of 20 competitor cells read `Not documented`; the table's own caption states this explicitly, and Astrix's sourced `No` (non-proxy, explicitly documented) is distinguished from Oasis's `Not documented` (no page found either way) |
| 4 | AgentGate claims link to shipped behavior and reproducible verification evidence | VERIFIED | Every AgentGate row cites `README.md`, `internal/gateway/gateway.go`, `internal/receipt`, or `cmd/agentgate-verify` — files that exist and are tested in this repository |
| 5 | The comparison contains no unsupported benchmarks, scores, or absolute market claims | VERIFIED | No numeric performance comparisons, rankings, or superlative market-position claims appear anywhere in the document; it is a presence/absence feature table only |

**Score:** 5/5 roadmap truths verified.

## Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| COMP-01 | SATISFIED | 5 products x 5 columns present |
| COMP-02 | SATISFIED | Every claim cited with a URL and fetch date |
| COMP-03 | SATISFIED | `Not documented` used consistently for absent evidence, distinguished from a sourced `No` |
| COMP-04 | SATISFIED | AgentGate's row cites this repository's own shipped code and docs |
| COMP-05 | SATISFIED | No benchmarks, scores, or market-share claims present |

No Phase 10 requirement is orphaned. REQUIREMENTS.md maps COMP-01 through COMP-05 to Phase 10 and R10.

## Scope Verification

New: `docs/comparison.md`,
`.planning/phases/10-sourced-product-comparison/{10-CONTEXT,10-01-SUMMARY,
10-VERIFICATION}.md`.

Modified: `.planning/{ROADMAP,REQUIREMENTS,PROJECT,STATE}.md`.

No Go code, configuration, or test file was touched. The contribution path
(Phase 11) is untouched.

## Behavioral Checks

| Check | Result |
|---|---|
| `go build ./...` | PASS (unaffected) |
| `go vet ./...` | PASS (unaffected) |
| `gofmt -l .` | clean, no output |
| Every competitor citation resolves to the page actually fetched during this phase | Confirmed at fetch time, 2026-08-16 |
| Astrix Security's Cisco/sales-status fact is present and prominent, not buried | Confirmed: blockquote at the top of its section |
| No prompt-injection instructions from `composio.dev` were followed | Confirmed: no signup performed, no credentials entered |

## Residual Risks

- No automated citation-freshness or link-liveness check exists; this is
  a manual maintenance obligation documented in `docs/comparison.md`'s own
  "Maintenance" section.
- No CI/GitHub Actions build wiring yet — unchanged from Phases 6-9.
