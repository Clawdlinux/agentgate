---
phase: 01-apache-2-0-release-basis
plan: 01
subsystem: release-governance
tags: [apache-2.0, licensing, notice, provenance]

requires: []
provides:
  - Apache License 2.0 source-release basis
  - Revision-bound owner relicense affirmation
  - Apache-compatible headers on the 4 previously BSL-marked Go files
affects: [receipt-protocol, oss-launch, contributor-entry]

tech-stack:
  added: []
  patterns:
    - Human legal authority is recorded separately from Git provenance.
    - Release metadata is checked byte-for-byte against reviewed sources.

key-files:
  created:
    - LICENSE
    - NOTICE
    - docs/relicense-authorization.md
  modified:
    - README.md
    - cmd/agentgw/main.go
    - internal/gateway/gateway.go
    - internal/registry/registry.go
    - internal/vault/vault.go

key-decisions:
  - "Owner affirmation is a tracked human statement. Git authorship and DCO sign-off are supporting provenance only."
  - "Phase 1 validates the source checkout. Binary dependency review remains separate."

patterns-established:
  - "License integrity: compare canonical text and source headers byte-for-byte."
  - "Human gate persistence: validate the exact owner edit, then commit only that file with sign-off."

requirements-completed:
  - LIC-01
  - LIC-02
  - LIC-03
  - LIC-04

duration: 27h 48m elapsed across checkpoint
completed: 2026-08-13
---

# Phase 1 Plan 1: Apache 2.0 Release Basis Summary

**Apache 2.0 source-release metadata with a committed, revision-bound owner authority affirmation**

## Performance

- **Duration:** 27h 48m elapsed across the owner checkpoint
- **Started:** 2026-08-12T02:53:19Z
- **Completed:** 2026-08-13T06:41:33Z
- **Tasks:** 4
- **Files modified:** 8 implementation files

## Accomplishments

- Added canonical Apache License 2.0 text and a minimal 2-line NOTICE.
- Replaced exactly 4 BSL headers without changing any Go runtime body.
- Recorded and committed the owner's exact relicense affirmation with DCO sign-off.
- Updated README license wording without legal interpretation or launch copy.

## Task Commits

Each implementation task was committed atomically:

1. **Task 1: Draft the revision-bound authority record** - `27052db`
2. **Task 2: Apply the exact Apache source-release metadata** - `adc6ad0`
3. **Task 3: Record the owner's personal authority affirmation** - human checkpoint
4. **Task 4: Commit and verify the owner's affirmation** - `03aa225`

## Files Created/Modified

- `LICENSE` - Canonical Apache License 2.0 text.
- `NOTICE` - AgentGate and Clawdlinux attribution.
- `docs/relicense-authorization.md` - Reviewed revision, scope, permission, and owner affirmation.
- `README.md` - Concise Apache License 2.0 statement and root link.
- `cmd/agentgw/main.go` - Apache-compatible source header.
- `internal/gateway/gateway.go` - Apache-compatible source header.
- `internal/registry/registry.go` - Apache-compatible source header.
- `internal/vault/vault.go` - Apache-compatible source header.

## Decisions Made

- Kept NOTICE limited to the locked AgentGate identity because no source-artifact evidence required extra notices.
- Limited R1 claims to source-checkout clearance. Binary distribution needs a separate dependency review.
- Preserved owner affirmation as a legal representation distinct from commit authorship and sign-off.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Corrected manual license transcription and text-file endings**
- **Found during:** Task 2 byte verification.
- **Issue:** The initial LICENSE transcription differed in 3 words. LICENSE and NOTICE lacked final newlines.
- **Fix:** Matched the reviewed sibling LICENSE byte-for-byte and added required final newlines.
- **Files modified:** `LICENSE`, `NOTICE`.
- **Verification:** `cmp -s` checks passed for both artifacts.
- **Committed in:** `adc6ad0`.

---

**Total deviations:** 1 auto-fixed bug.
**Impact on plan:** The repair enforced the planned byte-level integrity contract. Scope did not change.

## Issues Encountered

- The owner affirmation required 2 formatting corrections before the exact transformation validator passed.
- The final committed affirmation contains the required status, reviewer, and real calendar date.

## User Setup Required

None. No external service configuration is required.

## Verification Evidence

- `go test ./...` passed.
- All 4 Go body hashes matched the pinned pre-R1 revision.
- The implementation diff contained exactly the 8 planned paths outside `.planning/`.
- The source archive contained `LICENSE`, `NOTICE`, and the authority record exactly once.
- Every post-baseline commit passed `git show --check` and contained the exact sign-off trailer.
- The affirmation commit changed one file and exactly 2 fields.

## Next Phase Readiness

- Phase 1 clears the license blocker for Phase 2 planning and execution.
- Binary distribution claims remain blocked on a separate dependency-license review.

## Self-Check: PASSED

---

*Phase: 01-apache-2-0-release-basis*
*Completed: 2026-08-13*