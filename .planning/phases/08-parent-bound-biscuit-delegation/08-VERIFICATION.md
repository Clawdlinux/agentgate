---
phase: 08-parent-bound-biscuit-delegation
verified: 2026-08-16T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 8: Parent-Bound Biscuit Delegation Verification Report

**Phase Goal:** Agents can present attenuated authority that AgentGate verifies and binds to receipt lineage before dispatch.
**Verified:** 2026-08-16
**Status:** `passed`
**Re-verification:** No. This is the initial verification.

## Verdict

`internal/delegation` was built against the real `biscuit-go/v2.2.0` API
(confirmed live via `go get` and a throwaway probe test run before the
real implementation was written, which caught and fixed a real
`BlockCount`/`Code` indexing bug), wired into the gateway's request path
ahead of the pre-existing scope check, and verified end-to-end over real
HTTP: a verified delegation reaches the upstream and records its chain; a
mismatched-context token — the practical, constructible form of "splicing
a valid attenuation from one chain to widen another's scope" — is denied
before any registry, vault, or upstream access, and the denial itself is
receipted as auditable evidence rather than silently dropped.

## Goal Achievement

### Observable Truths

| # | Roadmap truth | Status | Evidence |
|---|---|---|---|
| 1 | AgentGate verifies each Biscuit token and request binding before registry, vault, or upstream access | VERIFIED | `executeAttempt` performs delegation verification as its first statement, before the scope check, the registry lookup, the vault fetch, and the upstream call; `TestAct_MismatchedDelegation_DeniedBeforeUpstreamButStillReceipted` proves zero upstream calls on denial |
| 2 | Delegation checks bind agent, principal, service, action, limits, expiry, and trusted root | VERIFIED | `Verifier.Verify` requires all four identity/action fields to match exactly, checks expiry against wall-clock time, and only accepts tokens whose full signature chain verifies against the configured root; `TestVerify_MismatchedContextDenied` (table-driven, one mismatch at a time), `TestVerify_ExpiredDenied`, `TestVerify_WrongRootKeyDenied` |
| 3 | Receipts keep direct grants empty and store only ordered commitments for valid attenuated lineage | VERIFIED | `attenuationChain` returns nil for a 0-attenuation-block token and one hex-encoded SHA-256 digest per attenuation block otherwise, never raw token bytes or block source text; `TestVerify_DirectGrantPasses`, `TestVerify_AttenuatedGrantPreservesOrderedLineage` |
| 4 | A chain-splicing test proves a grafted attenuation is rejected before dispatch | VERIFIED | `TestVerify_SplicedTokenRejectedForWrongContext` (a fully valid, unmodified token reused for a different request context) and `TestVerify_WideningViaAppendedFakeGrantFactRejected` (a holder-appended fake grant fact attempting to widen scope) both deny; `TestAct_MismatchedDelegation_DeniedBeforeUpstreamButStillReceipted` proves the same at the full gateway/HTTP level with zero upstream calls |
| 5 | Package documentation cites both named drafts as design context without claiming standards compliance | VERIFIED | `internal/delegation/doc.go` cites `draft-niyikiza-oauth-attenuating-agent-tokens` and `draft-oauth-ai-agents-on-behalf-of-user-02` by name and explicitly states neither is implemented as a wire format and no compliance claim is made |

**Score:** 5/5 roadmap truths verified.

## Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| DELG-01 | SATISFIED | Verification runs as the first step of `executeAttempt`, before any registry/vault/upstream access |
| DELG-02 | SATISFIED | All of principal/agent/service/action/expiry/root bound and checked |
| DELG-03 | SATISFIED | `delegation_chain` holds only SHA-256 digests, never raw tokens or block source |
| DELG-04 | SATISFIED | Direct grants (no attenuation) produce a nil chain; attenuated grants preserve ordered lineage |
| DELG-05 | SATISFIED | Two distinct splice/widen constructions both denied, at both the package level and the full gateway/HTTP level |
| DELG-06 | SATISFIED | `doc.go` cites both drafts, disclaims compliance |

No Phase 8 requirement is orphaned. REQUIREMENTS.md maps DELG-01 through DELG-06 to Phase 8 and R7.

## Scope Verification

New: `internal/delegation/{doc,rootstore,issue,verifier}.go`,
`internal/delegation/{rootstore_test,verifier_test}.go`,
`internal/gateway/delegation_test.go`,
`internal/db/migrations/003_delegation_root.sql`,
`.planning/phases/08-parent-bound-biscuit-delegation/{08-CONTEXT,
08-01-SUMMARY,08-VERIFICATION}.md`.

Modified: `internal/gateway/gateway.go` (`Config.Delegation`, `attempt`
gains `delegationToken`/`delegationChain`, delegation check moved into
`executeAttempt`, `extractDelegationToken` helper), `cmd/agentgw/main.go`
(root store + verifier wiring), `go.mod`/`go.sum` (adds
`github.com/biscuit-auth/biscuit-go/v2` and its transitive dependencies),
`.planning/{ROADMAP,REQUIREMENTS,PROJECT,STATE}.md`.

Google Workspace (Phase 9), sourced comparison (Phase 10), and the
contribution path (Phase 11) are untouched.

## Behavioral Checks

| Check | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l .` | clean, no output |
| `go test ./... -race -count=1` | PASS across every package, including the new `internal/delegation` package |
| `internal/delegation` package tests (13 tests) | PASS |
| `internal/gateway` delegation-specific tests (4 tests) | PASS |
| Exploratory probe test against the real biscuit-go v2.2.0 API | Ran, caught a real indexing bug, then removed before commit |

## Residual Risks

- No HTTP issuance endpoint for Biscuit grants — not required by any
  DELG-0X item.
- The root key does not rotate within this milestone.
- The `GetBlockID` widening defense is currently redundant with
  biscuit-go's own default Datalog-world scoping (documented explicitly,
  not hidden, in `08-CONTEXT.md`).
- No CI/GitHub Actions build wiring yet — unchanged from Phase 6/7.
