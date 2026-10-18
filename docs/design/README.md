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
| [0004](0004-merkle-checkpoints.md) | Evaluate Tessera with C2SP tiles and signed checkpoints | Proposed | 2026-10-18 |
| [0005](0005-root-certified-keys.md) | Certify short-lived replica keys with an organization root | Proposed | 2026-10-18 |
| [0006](0006-receipt-failure-modes.md) | Default closed; permit bounded durable spooling by opt-in | Proposed | 2026-10-18 |
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

## Review boundaries

Every decision remains proposed until owner review.
Implementation notes describe future acceptance tests, not completed validation.
Each ADR records tradeoffs and links its dependencies.
The sequencing document lists remaining owner decisions.
