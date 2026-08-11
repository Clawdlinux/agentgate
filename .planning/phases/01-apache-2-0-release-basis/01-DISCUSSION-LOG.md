# Phase 1: Apache 2.0 Release Basis - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md. This log preserves the alternatives considered.

**Date:** 2026-08-11
**Phase:** 01-apache-2-0-release-basis
**Areas discussed:** Relicense authority record, NOTICE identity, Go header coverage, README license wording

---

## Relicense Authority Record

| Option | Description | Selected |
|--------|-------------|----------|
| Tracked owner attestation | Human-reviewed authority record committed before merge | Yes |
| Git history only | Treat sole Git authorship as sufficient authority | No |
| Commit sign-off only | Treat DCO sign-off as relicensing permission | No |

**User's choice:** Delegated to Claude for a conservative autonomous decision.
**Notes:** Git history shows one author, but the context keeps explicit owner affirmation as the merge gate.

---

## NOTICE Identity

| Option | Description | Selected |
|--------|-------------|----------|
| Existing Clawdlinux identity | Use current project and copyright naming | Yes |
| Individual author identity | Replace the existing holder with the Git author | No |
| Unverified legal entity name | Add a corporate suffix not present in source | No |

**User's choice:** Delegated to Claude for a conservative autonomous decision.
**Notes:** Use `AgentGate` and `Copyright 2026 Clawdlinux.` without inventing legal status.

---

## Go Header Coverage

| Option | Description | Selected |
|--------|-------------|----------|
| Replace four incompatible headers | Change only files carrying BSL text | Yes |
| Add headers to every Go file | Apply Apache headers across all first-party Go files | No |
| Remove headers entirely | Rely only on root licensing files | No |

**User's choice:** Delegated to Claude for a conservative autonomous decision.
**Notes:** Avoid unrelated source churn. Repository-wide scans still block any remaining BSL text.

---

## README License Wording

| Option | Description | Selected |
|--------|-------------|----------|
| Short linked statement | Name Apache License 2.0 and link to `LICENSE` | Yes |
| Full license summary | Explain legal terms and patent effects | No |
| Badge only | Show a badge without direct prose | No |

**User's choice:** Delegated to Claude for a conservative autonomous decision.
**Notes:** Keep the existing section location. Phase 6 owns the later README quickstart restructure.

## Claude's Discretion

- Exact authority-record prose and headings within the locked evidence requirements.
- Exact validation command names and test placement.
- Minor wording choice for the README license sentence.

## Deferred Ideas

None.