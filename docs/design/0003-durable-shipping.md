# ADR-0003: SQLite outbox and signed sealed segments

## Status

Proposed

## Date

2026-10-18

## Context

The [ledger](../../internal/receipt/ledger.go) commits signed SQLite batches.
[WAL](../../internal/db/sqlite.go) is local storage, not replication.
[Exports](../../internal/receipt/export.go) provide signed snapshots, not shipping.
The [schema](../../internal/db/migrations/002_receipts.sql) prohibits receipt updates and deletes.
Clawdlinux requires air-gapped operation. Postgres is PARKED.

## Decision Drivers

- Continue local commits during destination outages.
- Ship verifiable artifacts, not live WAL files.
- Bound growth without deleting pending evidence.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| SQLite plus signed segments | Existing writer; local MinIO or filesystem | Async loss window; sealing implementation |
| Postgres outbox | Transactions and replication | PARKED; database and network operations |
| Kafka-like log | Replay and throughput | Quorum and partition operations |

Postgres and Kafka can run offline. Their extra operations, not cloud dependence, argue against initial adoption.

## Decision

Propose replica-local SQLite WAL plus a durable shipping outbox.
Ship immutable segments to configurable S3, in-cluster MinIO, or filesystem export.
Uploads never block request commits.

Seal after 4,096 records, 16 MiB, or 10 seconds, whichever comes first.
Also seal on epoch closure and drain.
Each segment covers 1 contiguous, nonempty chain range.
Store original receipt bytes. Never re-encode v1 receipts.
Capture existing SQLite history through the current v1 exporter, then preserve those bytes.
Never ship live SQLite, WAL, or SHM files.

A versioned descriptor binds chain, fencing token, range, count, anchor, terminal hash, and predecessor digest.
Also bind payload SHA-256 and certificate or legacy keyset digests.
Use separate signing domains and strict range validation. Bundle certificates; independently pin the root.

### Artifact Signers

Use [0005's purposes](0005-root-certified-keys.md).
Epoch keys sign descriptors with `segment_descriptor` and control records with `control`.
Destroy an epoch key only after its final segment descriptor and directory are durable.

The intermediate-certified singleton publisher signs cross-replica inventories and head vectors with `inventory`.
Replica keys cannot assert cross-replica coverage.
The intermediate signs the registry with `replica_registry`.
Inventories bind registry revision/digest, ranges, close records, and final descriptors.

The offline root and final v1 key co-sign migration descriptors using the same domain-separated preimage.
Bind original exports, final v1 head, legacy key history, and destination v2 identity.
A dedicated intermediate-certified archival publisher signs legacy descriptors with `legacy_segment`.
It references that migration envelope without re-signing or re-encoding v1 receipts.
Without migration, legacy verification still requires independently trusted v1 keys.

### Durable Materialization and Upload

Reserve range and outbox ID in a ledger-writer transaction.
Materialize immutable rows from a snapshot.
Write and fsync payload and descriptor, then atomically rename and fsync their directory.
Mark the outbox item sealed only after those files are durable.
Recreate interrupted reservations from retained rows. Uploads hold no SQLite write transaction.

Use object names containing chain ID, range, and payload digest.
Require conditional creation such as `If-None-Match: *`; object lock may add immutable retention.
Object lock alone must prevent replacement or bind references to immutable versions.
For filesystem export, create staging files with `O_CREAT|O_EXCL`, fsync, then publish through atomic no-replace linking.
Fsync the destination directory before publishing inventory references.
Retry identical names and bytes after uncertain acknowledgments; compare existing objects.
Verify the stored digest independently of multipart ETags.
Different existing bytes fail integrity.
Upload payload before descriptor; publish the inventory reference last.
Only the final reference advertises completeness.
Persist acknowledgments and inventory progress locally.

### Retention and Offline Consumption

Retain unuploaded segments and their source rows regardless of age.
Apply retention only after upload, read-back verification, and required publication.
Do not bypass current no-delete triggers.
The storage model remains an [owner decision](README.md#open-decisions):

- File per epoch: reclaim closed files after durable sealing, verified shipping, and required coverage.
- Single database: require an append-only-preserving compaction or archival migration before reclamation.

Recommend files per epoch, not yet implemented.
Neither model permits deleting pending intents or unshipped evidence.
Retain trusted anchors, certificates, and checkpoints as long as retained evidence needs them.
Quota exhaustion blocks admission rather than evicting pending evidence.

Verifiers validate inventory, descriptors, payloads, chains, and certificate bounds.
Sort segments by authenticated range, not object listing order.
Reject gaps and conflicting overlaps; identical retries are harmless.
Compare registered chains against coverage; report omissions.
[0004](0004-merkle-checkpoints.md) starts with head vectors, then optional compact proofs.
Inventory completeness is only relative to its signed capture point.

## Consequences

### Positive

Local admission tolerates upload outages.
Artifacts work through MinIO, S3, or removable media.

### Negative

Sealing needs implementation; off-node durability lags local acknowledgment.

### Risks and Mitigations

Volume loss loses unshipped evidence. Publish lag and agree acceptable loss.
Outages fill disks. Alert on capacity and stop admission before reserved outcome space disappears.
Storage can roll back inventories. Compare pinned coverage statements; otherwise report freshness unknown.

## Implementation Notes

1. Freeze descriptor, inventory, and outbox state-machine fixtures.
   Acceptance: original v1 export bytes and v2 signatures verify offline.
2. Inject crashes around reservation, fsync, rename, upload, and acknowledgment.
   Acceptance: retries never skip ranges or publish partial segments.
3. Race conflicting conditional puts and filesystem exclusive creates; lose upload replies against in-cluster MinIO.
   Acceptance: exactly 1 immutable object wins; matching retries succeed; conflicting bytes fail.
4. Test destination disconnection and quota pressure.
   Acceptance: no pending evidence is deleted, and retention never bypasses append-only triggers.
5. Rotate before the final descriptor is written, and attempt cross-replica signing with a replica certificate.
   Acceptance: the old key is retained; unauthorized inventory and legacy-segment signers fail.

## Related ADRs

- [0001: Epoch boundaries](0001-per-replica-chains.md).
- [0004: Publication](0004-merkle-checkpoints.md).
- [0005: Signing purposes](0005-root-certified-keys.md).
- [0006: Deferred direct spool shipping](0006-receipt-failure-modes.md).
