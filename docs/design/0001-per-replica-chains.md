# ADR-0001: Per-replica chains and epochs

## Status

Proposed

## Date

2026-10-18

## Context

The [ledger](../../internal/receipt/ledger.go) reads a single committed head inside each `BEGIN IMMEDIATE` batch.
Its primary key is currently [global `seq`](../../internal/db/migrations/002_receipts.sql) within 1 database.
The [signer store](../../internal/signer/store.go) explicitly supports 1 gateway writer per database.
Sharing a live database between replicas would violate that signing model.

[V1](../spec/RECEIPT-v1.md) authenticates neither a replica identifier nor an epoch.
Adding unsigned JSON metadata cannot turn either into authenticated identity.

## Decision Drivers

- Scale replicas without cross-replica SQLite locking.
- Make sequence reuse distinguishable after rotation or replacement.
- Preserve all v1 bytes and verification semantics.
- Support disconnected verification without a global sequencer.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Shared global chain | Simple total order | Shared writer bottleneck and coordination dependency |
| Per-replica chains without epochs | Local availability | Recovery and key replacement can reuse identities |
| Per-replica epoch chains | Local commits and explicit boundaries | Auditors must inspect several chains |

## Decision

Propose a required authenticated v2 field: `chain_id = <replica-id>:<epoch>`.
The [v2 outline](../spec/RECEIPT-v2.md) proposes its encoding boundaries.
`replica-id` is a provisioned UUID, not a reusable pod ordinal.
`epoch` is a positive, monotonically increasing integer persisted with the replica identity.
Each epoch starts at `seq = 1` with a zero previous hash.
The uniqueness key becomes `(chain_id, seq)`.

A healthy restart with intact storage resumes its existing epoch.
Key rotation creates a new epoch.
Lost storage requires a new replica UUID, even when Kubernetes reuses an ordinal.
An exclusive volume fence must prevent concurrent writers and restored-volume clones from reusing a live identity.
No proposal establishes a total execution order between replicas.

Propose signed epoch-open and epoch-close control records.
The opening record binds the preceding epoch's terminal hash and closing segment digest when available.
A missing predecessor is explicit, not presented as continuity.
Restoring an old backup requires fencing and a new replica UUID.

Propose a version-aware verifier that accepts v1 and v2.
Its v1 path retains the frozen preimages, signatures, and accepted semantics.
V1 receipts are never re-encoded during migration or shipping.
Keep their original JSONL bytes and existing verification vectors.
Existing v1-only binaries will still reject v2.

Migration closes the v1 ledger at a durable head.
A separately signed migration descriptor binds that head, its original export digest, and the new v2 chain.
That descriptor does not add fields to historical receipts.
The legacy export retains its original trust requirements.

## Consequences

### Positive

Each process retains a local group-commit writer.
Epoch identity separates key lifetimes and recovery histories.
Offline tooling can report missing epochs without querying a coordinator.

### Negative

Storage migrations must separate epoch metadata from the old global sequence model.
Chain aggregation and inventory become explicit operational work.

### Risks and Mitigations

Replica cloning can create forks. Require exclusive ownership and reject duplicate `(chain_id, seq)` values with different hashes.
An unpublished suffix can disappear. Compare signed inventories and checkpoints, not chain links alone.
Epoch resets could hide missing history. Require explicit predecessor references and report unverifiable gaps.

## Implementation Notes

1. Freeze v2 hash fields and chain-ID grammar with cross-language fixtures before implementing storage.
2. Add dual-version dispatch without changing the v1 parser or golden encodings.
   Acceptance: all existing v1 vectors retain their results.
3. Introduce durable replica identity, epoch allocation, and separate chain heads.
   Acceptance: concurrent replicas can both emit sequence 1 without collision.
4. Fence migration and rotation around writer admission.
   Acceptance: restart resumes the same head; rotation starts exactly 1 new epoch.
5. Exercise lost volumes, cloned backups, and interrupted migration.
   Acceptance: no chain identity is silently reused, and original v1 exports remain byte-identical.

## Related ADRs

- [0002: Lifecycle records](0002-two-phase-receipts.md) references intents by chain and hash.
- [0003: Segments](0003-durable-shipping.md) preserves epoch boundaries.
- [0005: Keys](0005-root-certified-keys.md) certifies epoch-scoped signing keys.
- [0007: Kubernetes](0007-kubernetes-lifecycle.md) provides persistent identity and fencing.
