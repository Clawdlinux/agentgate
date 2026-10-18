# ADR-0004: Signed head vectors before tiled proofs

## Status

Proposed

## Date

2026-10-18

## Context

[V1 verification](../../pkg/receiptspec/verifier.go) authenticates chains against keys and optional endpoints.
A valid prefix cannot expose every omission.
[agentgate-verify](../../cmd/agentgate-verify/main.go) has no Merkle proofs.

## Decision Drivers

- Expose missing chains.
- Publish offline, outside request commits.
- Prefer standard proof formats.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Signed `(chain_id, seq, head_hash)` vector plus inventories | Fewer dependencies; missing-chain coverage | Reads segments; no compact inclusion proofs |
| Trillian Tessera | Standard tiles; Go library; POSIX support | New dependency, publisher, and backend qualification |
| Hand-written Merkle tree | Small initial implementation | Team owns cryptographic correctness and recovery |
| Sigstore Rekor | Existing APIs and tooling | Supply-chain schema adaptation and service operations |

[Tessera](https://github.com/transparency-dev/tessera) succeeds older Trillian approaches.
[Rekor](https://github.com/sigstore/rekor) can run offline; its broader protocol is unnecessary here.

## Decision

### Initial Publication

Recommend signed vectors and inventories first; add Tessera when compact inclusion proofs justify its cost.
Vectors bind organization, installation, publication revision, predecessor digest, and registry revision.
Include every registered chain with its covered `(chain_id, seq, head_hash)`, close status, and inventory digest.
Explicitly list lost or uncovered chains.
A head vector is not a C2SP checkpoint.
Verify selected segments against its endpoints.

Propose publication every 10 seconds or 4,096 newly covered records, whichever comes first.
Publish descriptors and inventories before their vector.
Retain immutable vector history with conditional latest-pointer updates.
Coverage alone cannot prove freshness.

### Publisher Ownership and Custody

Use 1 logical publisher per installation with active/standby failover.
Its `inventory` key is intermediate-certified and cannot be a per-replica key.
Protect keys in local HSM, Vault transit, or approved encrypted storage.
Use a separate origin-scoped `checkpoint` key for the later log.
Keep both private keys outside gateway replicas.

Validate segments against the independently pinned root and scoped intermediate.
Check registry, fencing token, purpose, chains, digests, and terminal bounds.
Embedded certificates alone are not trusted roots.

The publisher acquires an external lease and monotonic fencing token.
Persist head map, predecessor digest, and any tree cursor before signing successors.
Publish conditionally against the exact predecessor.
Fence the former writer's storage before recovering its durable state on standby.
Kubernetes Lease expiry alone is insufficient.
Unproven fencing or continuity stops publication.
For POSIX Tessera, never attach its writable data volume to competing publishers.
Reconcile the last signed checkpoint before signing after failover.

### Later Merkle Profile

Tessera adds compact inclusion proofs and standard tooling, not missing-chain discovery.
Qualify a pinned POSIX release offline before adoption.
Do not assume a Tessera cloud backend supports MinIO.

Adopt [C2SP tlog-checkpoint v1.1.0](https://c2sp.org/tlog-checkpoint@v1.1.0)
and [tlog-tiles v1.0.0](https://c2sp.org/tlog-tiles@v1.0.0).
Use RFC 6962 SHA-256:
Leaf hashes are `SHA256(0x00 || leaf_bytes)`.
Internal hashes are `SHA256(0x01 || left_hash || right_hash)`.
Use signed-note Ed25519 checkpoints without extension lines.

Freeze leaf bytes binding receipt version, scope, sequence, and entry hash.
Use authenticated v2 `chain_id`, or v1's signed legacy-descriptor scope without modifying receipts.
Membership never replaces receipt signatures or chain verification.

Persist `(chain_scope, seq, entry_hash)` to leaf-index mappings.
Reconcile ambiguous integration before accepting retries; reject conflicting positions.
Integration order reflects arrival, not external execution order.

Checkpoint every 10 seconds or 4,096 integrated leaves, whichever comes first.
Publish tiles and entry bundles durably before publishing their checkpoint.
Retain immutable checkpoint history plus a mutable latest pointer.
Idle logs need not issue identical checkpoints.
Publish an inventory beside each checkpoint for per-chain coverage.

Extend `agentgate-verify` for local proofs and tiles.
Check certificate path, checkpoint signature/origin, leaf identity, inclusion, and tree size.
Check consistency against an earlier pinned checkpoint when supplied.
A single checkpoint proves neither freshness nor absence of split views.

Air-gapped publication starts with signed vector files, certificates, registry snapshots, and inventories.
The later log also exports signed checkpoint files and tiles.
Copy those artifacts and segments through approved removable media.
Verification requires no public services or network fetches.
Witness cosigning is optional and disabled by default.
Local witness quorum is optional. Without it, claim no witness-backed anti-equivocation.

## Consequences

### Positive

Vectors expose omissions without a tree service.
Later proofs reduce selective verification cost.

### Negative

Both stages need a fenced singleton publisher and durable state.
Initial selective verification is linear in the supplied segment range.

### Risks and Mitigations

A publisher can equivocate. Compare pinned checkpoints and optionally require independent local witness signatures.
Delayed publication can hide recent loss. Report integrated coverage separately from committed and uploaded heads.
Dependencies can change. Pin releases and run offline interoperability fixtures.

## Implementation Notes

1. Publish vectors from registry-backed inventories and fail over the active publisher during publication.
   Acceptance: no registered chain vanishes and no predecessor has competing accepted successors.
2. Lose the publisher node, block its lease renewal, and simulate failed storage fencing.
   Acceptance: an unfenced standby cannot publish; recovery preserves signed history.
3. Gate Tessera adoption on offline POSIX recovery and stable leaf indices after duplicate ingestion.
4. Add C2SP interoperability and CLI fixtures before enabling Merkle mode.
   Acceptance: wrong roots, origins, inclusion paths, or consistency paths fail; existing v1 results remain unchanged.

## Related ADRs

- [0001: Chains](0001-per-replica-chains.md) defines leaf identity.
- [0003: Segments](0003-durable-shipping.md) supplies validated inputs.
- [0005: Root keys](0005-root-certified-keys.md) certifies the log signing role.
