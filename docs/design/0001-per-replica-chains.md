# ADR-0001: Per-replica chains and epochs

## Status

Proposed

## Date

2026-10-18

## Context

The [ledger](../../internal/receipt/ledger.go) reads 1 head per `BEGIN IMMEDIATE` batch.
[Sequence](../../internal/db/migrations/002_receipts.sql) is global within its database.
The [signer](../../internal/signer/store.go) supports only 1 gateway writer per database.

[V1](../spec/RECEIPT-v1.md) authenticates neither a replica identifier nor an epoch.
Unsigned metadata cannot add authenticated identity.

## Decision Drivers

- Avoid cross-replica SQLite locks.
- Distinguish rotation and replacement.
- Preserve v1 bytes and offline verification.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Shared global chain | Simple total order | Shared writer bottleneck and coordination dependency |
| Per-replica chains without epochs | Local availability | Recovery and key replacement can reuse identities |
| Per-replica epoch chains | Local commits and explicit boundaries | Auditors must inspect several chains |

## Decision

Propose a required authenticated v2 field: `chain_id = <replica-id>:<epoch>`.
The [v2 outline](../spec/RECEIPT-v2.md) records encoding boundaries.
`replica-id` is a provisioned UUID, not a reusable pod ordinal.
Persist a positive, monotonically increasing `epoch` with replica identity.
The signed epoch-open control record is exactly `seq = 1`, with a zero previous hash.
The uniqueness key becomes `(chain_id, seq)`.

Intact restarts resume their epoch. Rotation starts another.
Lost storage requires a new UUID, even for reused ordinals.
There is no total cross-replica execution order.

### Fencing and Replica Authority

Propose an external epoch lease, checked before admission and every writer batch.
Use a Kubernetes Lease by default. Non-Kubernetes installations may use conditional object updates on an epoch-open lease.
The authoritative registry allocates monotonically increasing fencing tokens through compare-and-swap.
Lease acquisition requires the current workload identity, not merely a copied receipt certificate.
Include the token in epoch-open, subsequent records, and segment descriptors.
Lease loss or an uncertain renewal stops admission and further signing.
Registry handoffs bind the recovered head and token cutover boundary after storage fencing.
Publishers reject stale writes beyond that boundary, not legitimate historical receipts from the earlier token.
Already-dispatched upstream calls cannot be recalled by a lease.

The intermediate issuer refuses to certify the same `(replica UUID, epoch)` twice.
That rule alone cannot stop a clone reusing an existing certificate and private key.
The external lease and storage fencing are therefore required, not optional certificate features.
Kubernetes Lease ownership alone does not physically fence a partitioned volume writer.
Before takeover, establish storage fencing or refuse failover.

Propose an authoritative replica registry backed by an append-only, signed issued-certificate log.
Store it outside gateway PVCs, so replica loss cannot erase registration.
The intermediate signs registrations, certificate digests, fencing generations, replacements, and loss declarations.
Each entry binds organization, installation, replica UUID, epoch, and deployment slot.
Inventories bind the registry revision and digest; offline bundles include its signed snapshot and required log entries.
A registered chain with neither epoch-close nor inventory coverage is reported `lost`, even when no receipt survives.
This reports missing evidence, not proof that the replica never executed work.
Unclosed chains with partial coverage report the last authenticated head and an unknown suffix.
Missing registry history prevents a complete installation-level coverage claim.
Loss detection is relative to the supplied registry revision.
Without independently trusted freshness, unseen registrations cannot be ruled out.

Epoch-open references the predecessor's terminal hash and final segment digest when available.
Epoch-close is the final record and fixes the epoch's terminal sequence.
A missing predecessor is explicit. Restoring an old backup requires enrolment with a new UUID.

Propose dual-version verification with frozen v1 preimages, signatures, and accepted semantics.
V1 receipts are never re-encoded during migration or shipping.
Keep original JSONL bytes and vectors.
Existing v1-only binaries will still reject v2.

Migration closes the v1 ledger at a durable head.
A migration descriptor binds head, original exports, legacy keys, and new v2 chain.
The organization root and final v1 signing key must co-sign the same descriptor preimage.
Neither intermediate signatures nor embedded keys replace those approvals.
That descriptor does not add fields to historical receipts.
The legacy export retains its original trust requirements.

## Consequences

### Positive

Local group commit remains.
Epochs separate signing histories.
Offline tooling finds missing registered chains, not just surviving files.

### Negative

Storage must separate epoch heads.
Leases and registry add local dependencies.

### Risks and Mitigations

Replica cloning can create forks. Require external fencing and reject duplicate positions with different hashes.
An unpublished chain can disappear. Compare the issued-certificate log with signed inventory coverage.
Epoch resets could hide missing history. Require explicit predecessor references and report unverifiable gaps.

## Implementation Notes

1. Freeze v2 hash fields and chain-ID grammar with cross-language fixtures before implementing storage.
2. Add dual-version dispatch without changing the v1 parser or golden encodings.
   Acceptance: all existing v1 vectors retain their results.
3. Introduce durable replica identity, epoch allocation, and separate chain heads.
   Acceptance: concurrent replicas can both emit sequence 1 without collision.
4. Test concurrent lease acquisition, stale tokens, renewal loss, and copied certificates.
   Acceptance: only the fenced owner admits work; duplicate certificate issuance is refused.
5. Remove every segment of a registered chain.
   Acceptance: offline coverage reports it lost, rather than silently omitting it.
6. Interrupt migration and verify both co-signatures.
   Acceptance: either missing signature fails; v1 bytes remain identical; epoch-open occupies sequence 1.

## Related ADRs

- [0002: Lifecycle references](0002-two-phase-receipts.md).
- [0003: Segment boundaries](0003-durable-shipping.md).
- [0005: Issuance](0005-root-certified-keys.md).
- [0007: Identity and fencing](0007-kubernetes-lifecycle.md).
