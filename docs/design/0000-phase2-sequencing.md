# ADR-0000: Phase 2 sequencing

## Status

Proposed

## Date

2026-10-18

## Context

The [v1 contract](../spec/RECEIPT-v1.md) is frozen.
The [ledger](../../internal/receipt/ledger.go) and [signer](../../internal/signer/store.go) already coordinate group commits and rotation leases.
Phase 2 proposes new protocols around those guarantees.
These documents change no production code.

## Decision Drivers

- Preserve v1 evidence and offline verification.
- Separate schema agreement from implementation.
- Keep air-gapped deployments independent of cloud services.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Implement all proposals together | Few temporary interfaces | Hard failure attribution and broad review |
| Sequence contracts, then independent components | Smaller tests and clearer dependencies | Requires agreed artifact fixtures first |

## Decision

Propose this dependency order:

| Step | ADR | Prerequisites | Acceptance gate |
| --- | --- | --- | --- |
| 1 | [0001](0001-per-replica-chains.md) | None | Agree v2 chain identity and unchanged v1 fixtures |
| 2 | [0005](0005-root-certified-keys.md) | 0001 | Agree certificate scopes and epoch rotation fixtures |
| 3a | [0002](0002-two-phase-receipts.md) | 0001, 0005 | Verify durable intent ordering and crash recovery |
| 3b | [0003](0003-durable-shipping.md) | 0001, 0005 | Recover and verify sealed segments offline |
| 4a | [0004](0004-merkle-checkpoints.md) | 0003, 0005 | Verify segment receipts against offline checkpoints |
| 4b | [0006](0006-receipt-failure-modes.md) | 0002, 0003 | Prove bounded spool admission and exact replay |
| 5 | [0007](0007-kubernetes-lifecycle.md) | 0001, 0002, 0003, 0005, 0006 | Pass restart, drain, volume fencing, and outage tests |

0004 is not a prerequisite for Kubernetes ledger availability.
Kubernetes proof-publication tests require 0004 when that optional publisher is enabled.
0007 follows 0005 transitively through its other prerequisites.

## Consequences

### Positive

0002 and 0003 can be implemented independently after shared contracts settle.
0004 and 0006 can then proceed independently.
Tessera POSIX feasibility tests, benchmark reruns, and Kubernetes manifest drafts can start earlier.
V1 golden-vector regression work is independent of every new protocol.

### Negative

Final integration waits for stable fixtures and key provisioning.
Manifest-signer validity-window hardening remains separate work, not a prerequisite for preserving v1 bytes.

### Risks and Mitigations

Interface drift could invalidate parallel work. Freeze shared fixtures before merging dependent implementations.
Additional commits could worsen latency. Re-run Phase 0 workloads before accepting performance claims.

## Implementation Notes

1. Review [RECEIPT-v2](../spec/RECEIPT-v2.md) before any producer emits v2.
2. Land compatibility tests before new encoding or epoch storage.
3. Require each ADR's acceptance tests before enabling its feature.
4. Run integrated crash tests and disconnected verification before declaring Phase 2 usable.

### Owner Questions

- Who approves v2 fields and the v1-to-v2 migration envelope?
- What latency budget applies after the Phase 0 gate failed?
- What certificate lifetime and offline renewal interval fit each installation?
- What retention period, spool quota, and accepted unshipped-loss window apply?
- How will replicas provision consistent authorization and vault data currently stored in SQLite?
- Who operates the optional log publisher, and does any installation require witness quorum?

## Related ADRs

[Index](README.md) links all 7 proposed decisions.
