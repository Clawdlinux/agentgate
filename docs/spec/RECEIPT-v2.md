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
A migration descriptor binds original export digests, terminal head, legacy key history, and destination v2 chain.
The offline root and final v1 key co-sign the same preimage.
It does not rewrite old receipts or manufacture authenticated v1 chain IDs.
V2 and legacy manifests require separate explicit version dispatch.

## 2. Chain Identity

Propose required authenticated `chain_id = <replica-id>:<epoch>`.
`replica-id` is a lowercase provisioned UUID.
`epoch` is positive decimal without leading zeros.
Persist both; never derive cryptographic identity solely from a reusable Kubernetes ordinal.
The epoch-open control record occupies sequence 1 with a zero previous hash.
Within that chain, sequence and previous-hash checks remain mandatory.
No global ordering between replica chains is implied.

Proposed epoch-open records reference the preceding terminal hash and segment digest when available.
Key rotation creates a new epoch.
An intact restart resumes its current epoch.
Lost or restored storage requires fencing and a new replica UUID.
An external epoch lease and monotonic fencing token are checked before admission and each writer batch.
Bind that token into signed records and descriptors.
The issuer refuses duplicate `(UUID, epoch)` certification, but a copied certificate still requires lease-based fencing.
Offline coverage compares inventories with the intermediate-signed replica registry and issued-certificate log.
Registered chains with neither epoch-close nor inventory coverage are reported lost, even when no receipt survives.
Empty replacement PVCs require explicit issuer enrolment and a registry entry replacing the earlier UUID for that ordinal.

## 3. Proposed Record Kinds

All records authenticate their kind, chain identity, fencing token, sequence, previous hash, timestamp, and signing key.
Required action fields and optional result fields must be specified per kind.
Do not copy v1's required HTTP status into an intent with a fabricated value.

| Kind | Proposed authenticated content |
| --- | --- |
| `intent` | Attempt ID, optional client request ID, principal, agent, delegation, destination action, params digest, policy |
| `outcome` | Full intent reference, observation, upstream attempt count, known status, latency, bounded error code |
| `stream_result` | Header-outcome reference, available byte digest and count, termination classification |
| `decision` | Authenticated denied or rate-limited attempt without upstream dispatch |
| `control` | Typed mode changes or epoch boundaries with actor, reason, and bound configuration |

An intent reference includes chain ID, sequence, entry hash, and attempt ID.
Every attempt ID is gateway-generated and unique within its replica identity.
Specify its exact size and encoding before implementation.
Unknown outcomes use `outcome_unknown`, without invented provider status or successful completion.
Runtime-certain `not_dispatched` requires a known zero action-dispatch count.
Crash recovery cannot infer that state from an orphan intent.
Count action transport retries in `upstream_attempt_count`; unknown counts remain explicitly absent.
Propose a distinct observation field rather than extending v1's frozen policy enum.
Bind any validated client request ID into the intent.
Client retry deduplication and upstream `Idempotency-Key` mapping remain deferred.

SSE outcomes attest header acceptance before headers are forwarded.
Supplemental terminal records bind SHA-256 of bytes accepted by downstream writes.
They do not prove client delivery.
Recovery emits unknown outcomes or unknown stream termination without replaying upstream calls.
References may cross epoch boundaries.
Duplicate or contradictory terminal records must be rejected.
Draining streams after 40 seconds may produce unknown termination with reason `drain_deadline`.
That expected interruption is not a successful stream outcome.

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

Propose an offline organization root delegating to a longer-lived, in-cluster intermediate issuer.
The root delegation binds organization, installation, allowed purposes, and no further intermediate delegation.
The issuer certifies epoch keys and records each issuance in the signed replica registry.
Use v1-style length-prefixed binary certificate preimages, not canonical JSON.
Freeze domain strings, field order, `U32LE` lengths, `U64LE` integers, raw keys, and absence markers.
Ed25519 signs the SHA-256 certificate preimage digest.

| Purpose | Authorized scope and signer |
| --- | --- |
| `certificate_issuance` | Offline-root-delegated intermediate; permitted installation and child purposes only |
| `replica_registry` | Intermediate; registrations, issued certificates, fencing generations, replacements, and losses |
| `receipt` | Epoch key; action lifecycle records in exactly 1 UUID and epoch |
| `control` | Epoch key; epoch boundaries, authenticated mode changes, and future sink-transition markers |
| `segment_descriptor` | Epoch key; its epoch's signed segment ranges |
| `legacy_segment` | Intermediate-certified archival publisher; migrated v1 export scope |
| `inventory` | Intermediate-certified singleton publisher; cross-replica inventories and head vectors |
| `checkpoint` | Separately certified log key; installation and checkpoint origin |
| `trust_status` | Intermediate; delegated-key status and explicit sequence or epoch revocation cutoffs |
| `migration` | Offline root plus final v1 signing key; mandatory co-signatures |

Epoch and sequence bounds govern validity, not receipt clock values.
The certificate binds exactly 1 epoch starting at sequence 1.
Signed epoch-close fixes its inclusive terminal sequence.
Verifiers reject later records and require signed inventory coverage of close and final descriptor for terminal claims.
When Merkle mode is enabled, also prove epoch-close inclusion against its checkpoint.
Without that coverage, report an unclosed prefix or missing terminal evidence, not a complete chain.
Time windows are operational renewal hygiene only.
Recommend annual intermediate renewal and 24-hour epoch rotation.
Do not reject historical evidence merely because its time window elapsed.

Rotate at a writer batch boundary without draining admission or inflight calls.
Durably seal the old epoch's final descriptor before destroying its key.
The new epoch starts with epoch-open; later outcomes may reference prior-epoch intents.
Registry and activation recovery must prevent duplicate issuance or competing active epochs.

Recommend intermediate-signed trust-status statements every 6 hours, with a 24-hour freshness limit.
Bind revision, predecessor digest, affected certificates, and explicit revocation cutoffs.
Freshness requires trusted verifier time or a separately pinned revision; otherwise report freshness unknown.
Stale or missing status cannot produce a current-trust success claim.
Root-signed status handles intermediate replacement.
Preserve v1 trust behavior outside the co-signed migration envelope.

Proposed sealed segments contain original receipt bytes and signed range descriptors.
Descriptors bind payload digest, chain scope, anchor, terminal hash, predecessor segment, and certificate digests.
Specify export-container versions independently of receipt versions.
Offline bundles include required certificates and signed trust-status information.
They also include registry history and the signed inventory or head vector.
Their trust anchor is an independently pinned organization root, never an embedded self-asserted root.
Descriptors and inventories require conditional immutable creation; filesystem export requires exclusive creation and fsync.
The initial failure mode is `fail_closed`, returning 503 when required durable recording fails.
Spooling remains deferred until its covered failure class and external sink-transition marker protocol are approved.

## 6. Transparency Proof Profile

Recommend signed head vectors and inventories first. They do not provide compact Merkle inclusion proofs.
The fenced singleton publisher validates segment certificate paths against the independently pinned root.
Its durable predecessor state and storage fencing are required during failover.
For the later Tessera profile, propose C2SP [tlog-checkpoint](https://c2sp.org/tlog-checkpoint@v1.1.0)
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
4. Run crash recovery across intent, outcome, fencing, sealing, and batch-boundary epoch rotation.
5. Verify vectors offline; gate later Merkle mode on offline inclusion and consistency fixtures.
6. Document registry coverage, revocation freshness, storage-model retention, and certificate renewal targets.

The [open decisions](../design/README.md#open-decisions) record recommended defaults.
Proof freshness requires a separately trusted checkpoint or time policy.
No artifact alone proves that every external side effect was recorded.

## Related ADRs

- [0001: Per-replica chains](../design/0001-per-replica-chains.md)
- [0002: Intent and outcome](../design/0002-two-phase-receipts.md)
- [0003: Sealed segments](../design/0003-durable-shipping.md)
- [0004: Merkle checkpoints](../design/0004-merkle-checkpoints.md)
- [0005: Scoped issuer and epoch keys](../design/0005-root-certified-keys.md)
- [0006: Failure modes](../design/0006-receipt-failure-modes.md)
- [0007: Kubernetes lifecycle](../design/0007-kubernetes-lifecycle.md)
