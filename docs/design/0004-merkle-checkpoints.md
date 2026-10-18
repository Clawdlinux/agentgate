# ADR-0004: Tiled Merkle log and offline checkpoints

## Status

Proposed

## Date

2026-10-18

## Context

[V1 verification](../../pkg/receiptspec/verifier.go) authenticates a linear chain against supplied keys and an optional endpoint.
A valid prefix alone cannot detect a removed suffix or a conflicting view.
[agentgate-verify](../../cmd/agentgate-verify/main.go) currently has no Merkle inclusion or consistency checks.
Signed segments propose durable inputs, not an existing transparency log.

## Decision Drivers

- Provide compact inclusion evidence and append-only checkpoint comparisons.
- Use published formats instead of inventing tree cryptography.
- Publish inside an air gap without public Sigstore services.
- Keep request commits independent of proof publication.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Trillian Tessera | Go library; tiled log formats; upstream POSIX support | New dependency and publisher integration; backend qualification needed |
| Hand-written Merkle tree | Small initial code and direct SQLite integration | Team owns proof correctness, recovery, and long-term format maintenance |
| Sigstore Rekor | Existing transparency APIs and verification ecosystem | Supply-chain schema adaptation and extra service operations |

[Tessera](https://github.com/transparency-dev/tessera) is Trillian's tiled-log successor, not the older Trillian service stack.
[Rekor](https://github.com/sigstore/rekor) can be self-hosted. Public-network dependence is not an inherent requirement.
Its broader application protocol is unnecessary for the proposed receipt log.

## Decision

Propose Tessera behind a narrow asynchronous publisher interface.
Qualify a pinned release with its POSIX backend inside a disconnected cluster before adoption.
Do not assume S3 compatibility means every Tessera backend works with MinIO.
The segment shipper and tree backend are separate concerns.
If offline backend qualification fails, revisit this ADR before implementation.

Adopt [C2SP tlog-checkpoint v1.1.0](https://c2sp.org/tlog-checkpoint@v1.1.0)
and [tlog-tiles v1.0.0](https://c2sp.org/tlog-tiles@v1.0.0).
Use SHA-256 with RFC 6962 domain separation.
Leaf hashes are `SHA256(0x00 || leaf_bytes)`.
Internal hashes are `SHA256(0x01 || left_hash || right_hash)`.
Use standard signed-note checkpoint encoding with an Ed25519 log signature.
Do not invent JSON checkpoint signatures or optional extension lines.

Propose a small versioned leaf encoding binding receipt version, chain scope, sequence, and entry hash.
The v2 scope is its authenticated `chain_id`.
For v1, scope comes from the signed legacy segment descriptor, without modifying the receipt.
Specify leaf bytes and proof fixtures before implementation.
Tree membership does not replace receipt signatures or chain verification.

The publisher validates sealed segments before integration.
A durable mapping from `(chain_scope, seq, entry_hash)` to leaf index prevents upload retries from adding duplicate leaves.
Reject conflicting hashes for an already published chain position.
Cross-replica integration order records publisher arrival, not external execution order.
Crash recovery must reconcile ambiguous integration before accepting new work.

Propose a checkpoint every 10 seconds or 4,096 newly integrated leaves, whichever comes first.
Publish tiles and entry bundles durably before publishing their checkpoint.
Retain immutable checkpoint history plus a mutable latest pointer.
Idle logs need not issue identical checkpoints.
Checkpoint size authenticates coverage, not wall-clock freshness.
Publish a signed segment inventory alongside each checkpoint to expose per-chain coverage.

Propose `agentgate-verify` support for local proof bundles and tile directories.
Verify the root-certified log key, checkpoint origin and signature, leaf identity, inclusion path, and tree size.
Verify receipt signatures and segment boundaries separately.
Check consistency against an earlier trusted checkpoint when supplied.
A single checkpoint does not prove global freshness or absence of split views.

Air-gapped publication writes signed checkpoint files, tiles, certificates, and inventories to in-cluster storage.
Offline export copies those files and receipt segments through approved removable media.
No public Rekor, cloud KMS, or network fetch is required by verification.
Witness cosigning is optional and disabled by default.
An installation may configure local witnesses and a quorum policy.
Without that policy, make no witness-backed anti-equivocation claim.

## Consequences

### Positive

Proof verification can remain local and independent of the gateway process.
Published formats reduce custom cryptographic surface.

### Negative

Proof publication introduces asynchronous lag and another durable service role.
Tiles and historical checkpoints consume storage beyond the gateway volume.

### Risks and Mitigations

A publisher can equivocate. Compare pinned checkpoints and optionally require independent local witness signatures.
Delayed publication can hide recent loss. Report integrated coverage separately from committed and uploaded heads.
Dependency behavior can change. Pin releases and run disconnected interoperability fixtures before upgrades.

## Implementation Notes

1. Qualify Tessera POSIX recovery without external connectivity.
   Acceptance: restart preserves tree size, roots, and leaf indices.
2. Freeze leaf and checkpoint-key certificate formats.
   Acceptance: independent C2SP tooling verifies checkpoints and reconstructs tile proofs.
3. Test duplicate segment ingestion and crashes during integration.
   Acceptance: each unique chain position has 1 stable leaf index.
4. Add CLI fixtures for wrong roots, wrong origins, missing tiles, and invalid inclusion or consistency paths.
   Acceptance: offline checks reject each mutation while existing v1 verification remains unchanged.

## Related ADRs

- [0001: Chains](0001-per-replica-chains.md) defines leaf identity.
- [0003: Segments](0003-durable-shipping.md) supplies validated inputs.
- [0005: Root keys](0005-root-certified-keys.md) certifies the log signing role.
