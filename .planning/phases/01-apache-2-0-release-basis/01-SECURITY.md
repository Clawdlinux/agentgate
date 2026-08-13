---
phase: 01-apache-2-0-release-basis
audited: 2026-08-13
status: secured
threats_total: 4
threats_closed: 4
threats_open: 0
unregistered_flags: 0
asvs_level: not-applicable
baseline: 414f91792751b634f81f8a8046d961d9087709da
---

# Phase 01 Security Report

Phase 01 changes licensing metadata only. This audit verifies the 4 declared licensing threats.

ASVS is not applicable because this phase adds no runtime behavior or application security controls.

## Threat Verification

| Threat ID | Category | Disposition | Status | Evidence |
|---|---|---|---|---|
| T-01 | Spoofing | Mitigate | CLOSED | `docs/relicense-authorization.md` records Shreyansh Sancheti's affirmation, scope, and uncertainty gate. Commit `03aa225` changes only status and review date. Its date and exact sign-off passed validation. |
| T-02 | Tampering | Mitigate | CLOSED | `cmp -s LICENSE ../agentic-operator-core/LICENSE` passed. The root license is byte-identical to the reviewed sibling. |
| T-03 | Repudiation | Mitigate | CLOSED | NOTICE matches the locked 2-line value exactly. The authority inventory and Apache header scan both equal the 4 required Go paths. All body hashes match baseline. |
| T-04 | Spoofing | Mitigate | CLOSED | README contains exactly 1 concise Apache License 2.0 sentence. Its `[LICENSE](LICENSE)` link resolves to the root license. |

## T-01 Authority Evidence

The tracked record identifies the repository, baseline revision, effective date, owner, and complete first-party scope.

It lists exactly these former BSL paths:

- `cmd/agentgw/main.go`
- `internal/gateway/gateway.go`
- `internal/registry/registry.go`
- `internal/vault/vault.go`

It states that Git authorship and `Signed-off-by` do not establish legal authority.

It also states that incomplete or uncertain authority blocks merge.

Commit `03aa225152cd7d975241c9b5388ab8691b37c7f2` provides the tracked affirmation transition.

The commit changes only `docs/relicense-authorization.md`. Byte comparison proves exactly 2 fields changed.

The committed values are `Status: Affirmed` and `Review date: 2026-08-13`.

BSD `date` accepted and round-tripped the review date. The exact owner sign-off trailer is present.

## T-02 License Evidence

The root LICENSE matches `../agentic-operator-core/LICENSE` byte for byte.

The comparison used the reviewed sibling required by the threat model.

## T-03 Attribution Evidence

NOTICE contains exactly:

```text
AgentGate
Copyright 2026 Clawdlinux.
```

It has 1 final newline. No additional attribution was invented.

Exactly the 4 declared Go files contain the required Apache header.

No tracked Go file contains Business Source License, Business Source, or BSL text.

All 4 post-header SHA-256 values match the pinned baseline values.

## T-04 README Evidence

README retains exactly 1 `## License` section.

Its statement is: `AgentGate is licensed under the Apache License 2.0. See [LICENSE](LICENSE).`

No legal interpretation, patent summary, badge, or marketing claim was added.

## Threat Flags

The Phase 01 summary contains no `## Threat Flags` section. No unregistered implementation flag was reported.

The audited diff adds no runtime behavior. Runtime application threats are outside this licensing-only phase.

## Checks

- Exact T-01 authority transform, calendar date, sign-off, and uncertainty gate: PASS
- LICENSE sibling byte identity: PASS
- NOTICE exact bytes and final newline: PASS
- Exact authority inventory and Apache header path set: PASS
- 4 pinned Go body hashes: PASS
- Tracked Go BSL residue scan: PASS
- README exact wording and root link: PASS
- Exact 8-path non-planning phase diff: PASS
- Every post-baseline commit sign-off and whitespace check: PASS
- Source archive release-file membership: PASS
- `git diff --check`: PASS
- `go test ./...`: PASS

## Residual Boundaries

### Human Representation

Repository evidence proves the statement's content, transition, author, and sign-off.

It cannot independently prove ownership, assignment, employer consent, or the truth of the legal representation.

### Source-Only Scope

This audit covers the source checkout at the pinned baseline plus the licensing-only Phase 01 diff.

It does not clear binary distributions, compiled dependencies, or artifact-specific NOTICE obligations.

Those distribution claims require a separate dependency and artifact license review.

## Result

All 4 declared threat mitigations are present and verified. No declared threat remains open.