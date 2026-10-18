# AgentGate receipt v2: proposed outline

Status: Proposed. Not implemented. Not a frozen wire contract.
Date: 2026-10-18

This outline collects Phase 2 protocol decisions for review.
It is not sufficient for interoperable producers.
No production code in the baseline accepts v2.
The [v1 contract](RECEIPT-v1.md) remains frozen and independently verifiable.

## 1. Version Selection and Compatibility

Propose JSONL `format_version = 2` for v2 receipt records.
Use a new hash domain, proposed as `agentgate.receipt.hash.v2` followed by NUL.
Version dispatch must happen before selecting field validation or a hash preimage.
Never reinterpret v1 bytes under v2 rules.
Unknown versions must fail explicitly.

A future verifier accepts both versions using separate validation and encoding paths.
Existing v1-only binaries still reject v2.
Preserve v1 preimages, public test vectors, signatures, and accepted verification semantics.
Do not update v1 encoding locks to accommodate v2.
Never re-encode historical v1 receipts during migration or export.

Retain original v1 JSONL exports and their independently trusted key history.
A proposed root-signed migration descriptor may bind their exact payload digest and terminal head.
It does not rewrite old receipts or manufacture authenticated v1 chain IDs.
V2 and legacy manifests require separate explicit version dispatch.

## 2. Chain Identity

Propose required authenticated `chain_id = <replica-id>:<epoch>`.
`replica-id` is a lowercase provisioned UUID.
`epoch` is positive decimal without leading zeros.
Persist both; never derive cryptographic identity solely from a reusable Kubernetes ordinal.
Each chain starts at sequence 1 with a zero previous hash.
Within that chain, sequence and previous-hash checks remain mandatory.
No global ordering between replica chains is implied.

Proposed epoch-open records reference the preceding terminal hash and segment digest when available.
Key rotation creates a new epoch.
An intact restart resumes its current epoch.
Lost or restored storage requires fencing and a new replica UUID.

## 3. Proposed Record Kinds

All records authenticate their kind, chain identity, sequence, previous hash, timestamp, and signing key.
Required action fields and optional result fields must be specified per kind.
Do not copy v1's required HTTP status into an intent with a fabricated value.

| Kind | Proposed authenticated content |
| --- | --- |
| `intent` | Attempt ID, principal, agent, delegation, destination action, params digest, policy |
| `outcome` | Full intent reference, observation class, known status, latency, bounded error code |
| `stream_result` | Header-outcome reference, available byte digest and count, termination classification |
| `decision` | Authenticated denied or rate-limited attempt without upstream dispatch |
| `control` | Typed mode changes or epoch boundaries with actor, reason, and bound configuration |

An intent reference includes chain ID, sequence, entry hash, and attempt ID.
Every attempt ID is gateway-generated and unique within its replica identity.
Specify its exact size and encoding before implementation.
Unknown outcomes use `outcome_unknown`, without invented provider status or successful completion.
Propose a distinct observation field rather than extending v1's frozen policy enum.

SSE outcomes attest header acceptance before headers are forwarded.
Supplemental terminal records bind SHA-256 of bytes accepted by downstream writes.
They do not prove client delivery.
Recovery emits unknown outcomes or unknown stream termination without replaying upstream calls.
References may cross epoch boundaries.
Duplicate or contradictory terminal records must be rejected.

The [earlier stream proposal](proposals/receipt-stream-digest.md) remains unimplemented.
This outline proposes replacing its single-action-receipt rule for v2 lifecycle records.

## 4. Hashing and Signature Work Remaining

Propose SHA-256 entry hashes and Ed25519 receipt signatures over entry hashes.
Authenticate version and kind under the new domain.
Include chain identity and lifecycle references in the signed preimage.
Keep gateway params canonicalization unchanged unless separately versioned and reviewed.
Do not treat the narrower decision-record canonicalizer as interchangeable with gateway params.

Before acceptance, specify exact field order, integer widths, endianness, string prefixes, and absence markers.
Also freeze string bounds, enum values, timestamp rules, attempt-ID grammar, and cross-field validation.
Assign separate domains to certificates, segment descriptors, inventories, and migration descriptors.
Define strict duplicate-field and unknown-field handling for new signed containers.
No placeholder encoding in this outline authorizes production emission.

## 5. Keys and Export Artifacts

Propose root-certified replica keys scoped to exactly 1 replica UUID and epoch.
Certificates bind purpose, public key, and validity under a versioned minimal signed statement.
A separately scoped log certificate authorizes checkpoint signatures for a specific origin.
Preserve v1 legacy trust behavior outside an explicit migration envelope.

Proposed sealed segments contain original receipt bytes and signed range descriptors.
Descriptors bind payload digest, chain scope, anchor, terminal hash, predecessor segment, and certificate digests.
Specify export-container versions independently of receipt versions.
Offline bundles include required certificates and signed trust-status information.
Their trust anchor is an independently pinned organization root, never an embedded self-asserted root.

## 6. Transparency Proof Profile

Propose C2SP [tlog-checkpoint](https://c2sp.org/tlog-checkpoint@v1.1.0)
and [tlog-tiles](https://c2sp.org/tlog-tiles@v1.0.0) with SHA-256.
Freeze leaf bytes binding receipt version, authenticated chain scope, sequence, and entry hash.
Legacy scope comes from its signed descriptor, not modified v1 receipts.
Specify inclusion bundles and consistency proofs with independent fixtures.
Use standard signed-note checkpoint signatures.
Export checkpoint files, tiles, inventories, certificates, and receipts for disconnected verification.
Witness quorum remains optional.

## 7. Acceptance Gates and Open Questions

1. Approve a complete byte-level specification and deterministic vectors for each record and artifact kind.
2. Preserve all v1 vector outcomes and byte locks under dual-version verification.
3. Reject mismatched chain identity, duplicate terminal references, conflicting segments, and unauthorized certificate purposes.
4. Run crash recovery across intent, outcome, spool, sealing, and epoch rotation boundaries.
5. Verify inclusion and consistency proofs with no network or private keys.
6. Document clock limitations, revocation freshness, retention, and owner-approved signing lifetimes.

Open questions include certificate renewal policy, accepted publication lag, and archival retention.
Proof freshness requires a separately trusted checkpoint or time policy.
No artifact alone proves that every external side effect was recorded.

## Related ADRs

- [0001: Per-replica chains](../design/0001-per-replica-chains.md)
- [0002: Intent and outcome](../design/0002-two-phase-receipts.md)
- [0003: Sealed segments](../design/0003-durable-shipping.md)
- [0004: Merkle checkpoints](../design/0004-merkle-checkpoints.md)
- [0005: Root-certified keys](../design/0005-root-certified-keys.md)
- [0006: Failure modes](../design/0006-receipt-failure-modes.md)
- [0007: Kubernetes lifecycle](../design/0007-kubernetes-lifecycle.md)
