# Phase 1: Apache 2.0 Release Basis - Context

**Gathered:** 2026-08-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Establish an accurate Apache License 2.0 release basis for the existing AgentGate repository. This phase records owner authority, adds `LICENSE` and `NOTICE`, replaces the four incompatible BSL headers, and updates the README license statement. It does not change runtime behavior or implement receipts.

</domain>

<decisions>
## Implementation Decisions

### Relicense Authority Record
- **D-01:** Create a tracked `docs/relicense-authorization.md` record before the relicense merges.
- **D-02:** The record must identify the repository, the distributed first-party code in scope, the current BSL-marked files, the effective date, and the owner granting Apache License 2.0 permission.
- **D-03:** Shreyansh Sancheti must personally review and affirm the authority statement. Git authorship and `Signed-off-by` trailers are supporting evidence, not legal authorization.
- **D-04:** The phase remains blocked if the owner cannot confirm authority over all distributed first-party code. Planning and mechanical preparation may proceed, but merge approval may not.

### NOTICE Identity
- **D-05:** Use the existing source identity without inventing a legal suffix: `AgentGate` and `Copyright 2026 Clawdlinux.`
- **D-06:** Keep `NOTICE` minimal. Add third-party notices only when a distributed artifact creates an actual notice obligation supported by license evidence.

### Go Header Coverage
- **D-07:** Replace the incompatible headers in exactly these four files: `cmd/agentgw/main.go`, `internal/gateway/gateway.go`, `internal/registry/registry.go`, and `internal/vault/vault.go`.
- **D-08:** Use the established 4-line Apache header from `../agentic-operator-core/pkg/audit/chain.go`, preserving `Copyright 2026 Clawdlinux.`
- **D-09:** Do not add headers to unrelated headerless Go files. The release scan proves no incompatible BSL text remains across all first-party Go files.

### README License Wording
- **D-10:** Keep the existing `## License` section and replace its BSL sentence with a concise Apache License 2.0 statement linked to `LICENSE`.
- **D-11:** Do not add legal interpretation, patent summaries, badges, or open-source marketing claims in this phase.

### Claude's Discretion
- Exact prose and headings inside the authority record, provided D-01 through D-04 remain explicit.
- Exact validation command names and test placement.
- Whether the `LICENSE` link sentence says "licensed under" or "available under."

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Product And Phase Scope
- `PRD-receipts-oss.md` — Defines R1, fixed task order, blockers, and done criteria.
- `.planning/PROJECT.md` — Defines the milestone boundary, legal constraint, and existing runtime baseline.
- `.planning/REQUIREMENTS.md` — Defines LIC-01 through LIC-04 and exact traceability.
- `.planning/ROADMAP.md` — Defines the Phase 1 goal, merge gate, and success criteria.

### Established License Header
- `../agentic-operator-core/pkg/audit/chain.go` — Provides the exact Apache header pattern requested by the PRD.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `../agentic-operator-core/LICENSE`: Use the sibling Apache-licensed project as a local source check, while the canonical license text must remain unmodified.
- `../agentic-operator-core/pkg/audit/chain.go`: Reuse its existing Apache header form.

### Established Patterns
- Current repository history and all commits touching the four BSL-marked files have one Git author: Shreyansh Sancheti. This supports review but does not replace D-03.
- Existing source headers identify the copyright holder as `Clawdlinux`.
- The repository currently has no root `LICENSE` or `NOTICE` file despite header and README links to `LICENSE`.

### Integration Points
- `cmd/agentgw/main.go`: Replace BSL header only.
- `internal/gateway/gateway.go`: Replace BSL header only.
- `internal/registry/registry.go`: Replace BSL header only.
- `internal/vault/vault.go`: Replace BSL header only.
- `README.md` § License: Replace the BSL statement and retain a direct root license link.
- `docs/relicense-authorization.md`: New human-reviewed merge-gate evidence.

</code_context>

<specifics>
## Specific Ideas

- The user delegated implementation choices and asked for conservative autonomous decisions.
- The authority record must never imply that commit authorship automatically proves relicensing rights.

</specifics>

<deferred>
## Deferred Ideas

None. Discussion stayed within Phase 1.

</deferred>

---

*Phase: 01-apache-2-0-release-basis*
*Context gathered: 2026-08-11*