---
phase: 1
slug: apache-2-0-release-basis
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-08-12
---

# Phase 1 - Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go test plus shell release checks |
| **Config file** | None |
| **Quick run command** | `go test ./cmd/agentgw ./internal/gateway ./internal/registry ./internal/vault` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | Under 30 seconds |

---

## Sampling Rate

- **After every task commit:** Run the task's release checks and quick Go tests.
- **After every plan wave:** Run `go test ./...` and `git diff --check`.
- **Before `/gsd:verify-work`:** Full tests, release checks, and manual owner affirmation must pass.
- **Max feedback latency:** 30 seconds for automated checks.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 01-01-01 | 01 | 1 | LIC-01 | T-01 | Authority is recorded but never inferred from Git metadata. | text plus manual gate | `test -s docs/relicense-authorization.md && rg -n 'AgentGate|github.com/Clawdlinux/agentgate|Apache License 2\.0|Shreyansh Sancheti' docs/relicense-authorization.md` | No, Wave 0 | pending |
| 01-01-02 | 01 | 1 | LIC-02 | Canonical license text and factual NOTICE ship unchanged. | release check | `cmp -s LICENSE ../agentic-operator-core/LICENSE && test "$(cat NOTICE)" = $'AgentGate\nCopyright 2026 Clawdlinux.'` | No, Wave 0 | pending |
| 01-01-02 | 01 | 1 | LIC-03 | Only the 4 locked Go files receive Apache headers. No Go BSL text remains. | release check plus regression | `test "$(git ls-files '*.go' | xargs rg -l 'Licensed under the Apache License, Version 2\.0\.' | sort | wc -l | tr -d ' ')" = 4 && ! git ls-files '*.go' | xargs rg -n 'Business Source License|Business Source|BSL' && go test ./cmd/agentgw ./internal/gateway ./internal/registry ./internal/vault` | Existing files need edits | pending |
| 01-01-02 | 01 | 1 | LIC-04 | README names Apache License 2.0 and links the root license. | release check | `rg -n '^## License$' README.md && rg -n 'Apache License 2\.0.*\[LICENSE\]\(LICENSE\)' README.md` | Existing file needs edit | pending |

---

## Wave 0 Requirements

- [ ] `docs/relicense-authorization.md` with owner-review fields for LIC-01.
- [ ] Root `LICENSE` with canonical Apache License 2.0 text.
- [ ] Root `NOTICE` with the locked 2-line identity.
- [ ] Release-check commands embedded in plan verification and the review checklist.

No new test framework or package is required.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Owner authority affirmation | LIC-01 | Repository metadata cannot prove ownership, assignment, or permission. | Shreyansh Sancheti personally reviews the tracked record. Confirm repository, revision, first-party scope, 4 BSL paths, effective date, Apache 2.0 permission, typed name, and review date. Block merge on uncertainty. |
| Source-only claim boundary | LIC-02 | Dependency obligations depend on the distributed artifact. | Confirm Phase 1 validates the source checkout only. Do not claim binary-release clearance without a separate dependency review. |

---

## Validation Sign-Off

- [x] All planned tasks have automated verification or Wave 0 dependencies.
- [x] Sampling continuity has no 3 consecutive tasks without automated verification.
- [x] Wave 0 covers every missing artifact.
- [x] Commands use no watch-mode flags.
- [x] Automated feedback latency is under 30 seconds.
- [x] `nyquist_compliant: true` is set in frontmatter.

**Approval:** pending owner authority affirmation