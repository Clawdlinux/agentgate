# AgentGate Phase 2 decisions

Status: Proposed
Date: 2026-10-18

AgentGate is part of Clawdlinux, an air-gapped agent governance and attestation plane.
These documents propose Phase 2 designs. They do not describe shipped features.
Air-gapped operation and offline verification are required. No proposal requires a public cloud.

The code baseline is `origin/main` at `d740dc6428865d0ca6e4998d769159f461a2fdfb`.
The [v1 wire contract](../spec/RECEIPT-v1.md) remains frozen.
The [v2 outline](../spec/RECEIPT-v2.md) is a proposal, not an interoperable specification.

## Index

| ADR | Proposed decision | Status | Date |
| --- | --- | --- | --- |
| [0000](0000-phase2-sequencing.md) | Dependency order and independent work | Proposed | 2026-10-18 |
| [0001](0001-per-replica-chains.md) | Separate replica epochs with authenticated v2 chain IDs | Proposed | 2026-10-18 |
| [0002](0002-two-phase-receipts.md) | Commit intent before dispatch and outcome before response | Proposed | 2026-10-18 |
| [0003](0003-durable-shipping.md) | Ship signed segments from replica-local SQLite | Proposed | 2026-10-18 |
| [0004](0004-merkle-checkpoints.md) | Publish signed head vectors first; defer Tessera proofs | Proposed | 2026-10-18 |
| [0005](0005-root-certified-keys.md) | Delegate an offline root to a scoped local epoch-key issuer | Proposed | 2026-10-18 |
| [0006](0006-receipt-failure-modes.md) | Implement fail-closed first; defer spooling pending a named failure class | Proposed | 2026-10-18 |
| [0007](0007-kubernetes-lifecycle.md) | Use StatefulSet identities and isolated WAL volumes | Proposed | 2026-10-18 |

## Current implementation

- [Receipt protocol](../../pkg/receiptspec/receipt.go) and [encoding](../../pkg/receiptspec/encoding.go) support v1 only.
- [Ledger](../../internal/receipt/ledger.go) uses a single writer goroutine per ledger.
- Production creates 1 ledger per process. Each batch uses `BEGIN IMMEDIATE` and replies after `COMMIT`.
- [SQLite configuration](../../internal/db/sqlite.go) requests WAL mode.
- [Signer store](../../internal/signer/store.go) caches private keys and excludes rotation during batch leases.
- [Gateway](../../internal/gateway/gateway.go) commits after upstream execution, before returning its response.
- [SSE forwarding](../../internal/gateway/stream.go) starts after the header-time receipt commits.
- [Offline verifier](../../cmd/agentgate-verify/main.go) reads SQLite or JSONL. It does not verify Merkle proofs.
- [Compose](../../docker-compose.yaml) mounts local data. This baseline has no Helm chart or Kubernetes workload manifests.

The [Phase 0 benchmark PR](https://github.com/Clawdlinux/agentgate/pull/50) contains results absent from this baseline.
Its reported latency gate is not met.
Phase 2 must not claim that extra durable writes meet that gate.

## Open decisions

The owner must approve these choices before dependent features are enabled.
Defaults below are recommendations, not shipped configuration.
All certificate and epoch changes remain subject to the frozen v1 compatibility boundary.

| Decision | Recommended default | Related ADRs |
| --- | --- | --- |
| V2 protocol approval | Assign a protocol owner; approve binary preimages and fixtures before emitting v2. | [0001](0001-per-replica-chains.md), [v2](../spec/RECEIPT-v2.md) |
| Fencing backend and timing | Kubernetes Lease with registry-allocated tokens; renew every 5 seconds within a 15-second lease. Require storage fencing before takeover. | [0001](0001-per-replica-chains.md), [0007](0007-kubernetes-lifecycle.md) |
| Issuer custody and renewal | Offline root; local HSM or Vault transit for the intermediate. Renew annually; target epoch rotation every 24 hours. | [0005](0005-root-certified-keys.md) |
| Revocation scope and freshness | Intermediate-signed status every 6 hours, valid for freshness checks for 24 hours. Use explicit epoch or sequence cutoffs. | [0005](0005-root-certified-keys.md) |
| SQLite storage model | File per epoch, enabling whole-file archival. A single database instead needs an approved append-only-preserving reclamation migration. | [0003](0003-durable-shipping.md) |
| Evidence retention | Propose 30 days locally and 365 days in verified archive. Never delete unshipped evidence or required trust material. | [0003](0003-durable-shipping.md) |
| Publication profile | Signed head vectors and inventories first. Enable Tessera only after compact-proof demand and offline backend qualification. | [0004](0004-merkle-checkpoints.md) |
| Publisher custody and failover | Separate protected publisher keys; 1 active writer with storage-fenced standby. Refuse unproven takeover. | [0004](0004-merkle-checkpoints.md) |
| Accepted unshipped-loss risk | Target 10-second healthy shipping. Require explicit acceptance of asynchronous suffix loss; do not claim zero-loss durability. | [0003](0003-durable-shipping.md) |
| Replica authorization and vault state | Keep replicas at 1 until local provisioning, credential rotation, and revocation propagation pass cross-replica tests. | [0007](0007-kubernetes-lifecycle.md) |
| Client retry semantics | Bind client request IDs now. Defer retry deduplication and provider idempotency-key mapping; never automatically replay unknown outcomes. | [0002](0002-two-phase-receipts.md) |
| Spool failure class | Keep `fail_closed` and reject `fail_open_spool`. Reconsider only after a named fault and independent durability test exist. | [0006](0006-receipt-failure-modes.md) |
| Added-latency budget | Retain the under-1-ms p99 objective pending owner review. Claim no pass without dedicated Linux measurements. | [0002](0002-two-phase-receipts.md) |
| Long-stream rollout policy | Keep the 40-second drain budget. Document stream cuts and expected unknown termination instead of promising uninterrupted SSE. | [0007](0007-kubernetes-lifecycle.md) |
| Witness policy | Disabled by default. Require explicit local quorum policy before making witness-backed anti-equivocation claims. | [0004](0004-merkle-checkpoints.md) |

Issuer software and certificate-format approval do not solve replica-state distribution.
Multi-replica AgentGate remains blocked until that separate state problem is solved.
Untrusted clocks cannot prove status freshness; report unknown unless verifier time or a status revision is independently trusted.

## Review boundaries

Every decision remains proposed until owner review.
Implementation notes describe future acceptance tests, not completed validation.
Each ADR records tradeoffs and links its dependencies.
The [sequencing document](0000-phase2-sequencing.md) separates prerequisites from deferred features.
