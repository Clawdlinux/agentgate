# ADR-0003: SQLite outbox and signed sealed segments

## Status

Proposed

## Date

2026-10-18

## Context

The [ledger](../../internal/receipt/ledger.go) already serializes signed batches into SQLite.
[WAL configuration](../../internal/db/sqlite.go) provides local storage, not off-node replication.
Current [exports](../../internal/receipt/export.go) provide signed snapshot ranges, not durable shipping.
The [schema](../../internal/db/migrations/002_receipts.sql) rejects receipt updates and deletes.

AgentGate belongs to Clawdlinux's air-gapped governance and attestation plane.
Postgres is PARKED for Phase 2.
No storage design may require public-cloud connectivity.

## Decision Drivers

- Preserve local commits during destination outages.
- Ship independently verifiable artifacts without exporting raw live WAL files.
- Bound disk growth without silently deleting unshipped evidence.
- Support disconnected installations.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Replica SQLite plus signed segments | Fits current writer; S3 or in-cluster MinIO; disconnected export | Asynchronous loss window; custom sealing and replay |
| Postgres transactional outbox | Shared SQL transactions and mature replication | PARKED; requires database operations and network availability |
| Kafka-like durable log | Replay and partitioned throughput | Additional quorum, retention, and partition ownership operations |

Postgres and Kafka-like systems can run offline. Cloud dependence is not the reason to reject them.
They add dependencies before the artifact protocol settles.

## Decision

Propose replica-local SQLite WAL plus a durable shipping outbox.
Ship signed immutable segments to configurable S3-compatible storage.
Support in-cluster MinIO and filesystem export for fully disconnected installations.
Postgres remains PARKED.
Uploads stay outside the request commit path.

Seal at the proposed initial limits of 4,096 records, 16 MiB, or 10 seconds, whichever comes first.
Also seal on epoch closure and graceful drain.
Each segment covers a contiguous, nonempty range from exactly 1 chain.
Store original receipt bytes. Never re-encode v1 receipts.
For existing SQLite history, capture its initial v1 export with the current exporter and preserve that artifact unchanged.
Do not ship a live SQLite database, WAL, or SHM file as a segment.

A versioned signed descriptor binds chain identity, range, count, anchor, terminal hash, and previous segment digest.
Also bind payload SHA-256 and certificate or legacy keyset digests.
Use a separate signing domain and strict range validation, not an undocumented extension of v1 manifests.
Bundle certificates, but independently pin the trusted root.

The ledger writer reserves a segment range and outbox ID transactionally.
A read snapshot supplies immutable rows for deterministic materialization.
Write and fsync payload and descriptor, then atomically rename and fsync their directory.
Mark the outbox item sealed only after those files are durable.
An interrupted reservation is recreated from retained rows after restart.
Uploads never hold the SQLite write transaction.

Use object names containing chain ID, range, and payload digest.
Retry the same name and bytes after uncertain acknowledgments.
Verify the stored digest independently of multipart ETags.
A pre-existing different object fails integrity checks.
Upload payload before descriptor; publish the inventory reference last.
Only the final reference advertises completeness.
Persist acknowledgments and inventory progress locally.

Retain unuploaded segments and their source rows regardless of age.
Apply an owner-set retention period only after upload, read-back verification, and any required proof publication.
Do not bypass current no-delete triggers.
Reclaim whole closed epoch databases and segment files through a future archival workflow.
Retain trusted anchors, certificates, and checkpoints as long as retained evidence needs them.
Quota exhaustion blocks admission rather than evicting pending evidence.

Verifiers read a signed inventory, then validate descriptors, payload digests, chain links, and epoch certificates.
Sort segments by authenticated range, not object listing order.
Reject gaps and conflicting overlaps; identical retries are harmless.
Optional proofs bind these receipts to [0004 checkpoints](0004-merkle-checkpoints.md).
Inventory completeness is only relative to its signed capture point.

## Consequences

### Positive

Local admission does not depend on storage network availability.
The same artifacts work through MinIO, S3, or removable media.

### Negative

Sealing and archival recovery require new implementation.
Off-node durability lags local acknowledgment.

### Risks and Mitigations

A lost volume can lose an unshipped suffix. Publish lag and agree an accepted loss window.
Destination outages can fill disks. Alert on age and capacity, then stop admission before reserved outcome space disappears.
Malicious storage can roll back inventory. Compare separately pinned checkpoints and report freshness as unknown when unpinned.

## Implementation Notes

1. Freeze descriptor, inventory, and outbox state-machine fixtures.
   Acceptance: original v1 export bytes and v2 signatures verify offline.
2. Inject crashes around reservation, fsync, rename, upload, and acknowledgment.
   Acceptance: retries never skip ranges or publish partial segments.
3. Test conflicting object names and lost upload replies against in-cluster MinIO.
   Acceptance: exact retries succeed; conflicting bytes fail verification.
4. Test destination disconnection and quota pressure.
   Acceptance: no pending evidence is deleted, and retention never bypasses append-only triggers.

## Related ADRs

- [0001: Epochs](0001-per-replica-chains.md) defines segment boundaries.
- [0004: Merkle publication](0004-merkle-checkpoints.md) consumes sealed segments.
- [0005: Certificates](0005-root-certified-keys.md) authorizes descriptor signers.
- [0006: Spool](0006-receipt-failure-modes.md) feeds the same shipping path.
