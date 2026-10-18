# ADR-0005: Offline root, scoped issuer, and epoch keys

## Status

Proposed

## Date

2026-10-18

## Context

The [signer store](../../internal/signer/store.go) caches Ed25519 keys.
`WithActiveKey` leases exclude rotation through batch commit.
`Rotate` switches global sequence windows and clears old keys, without epochs or certificates.

[V1](../spec/RECEIPT-v1.md) trusts keys with inclusive sequence windows.
[Manifest verification](../../pkg/receiptspec/manifest.go) currently omits signer-window checks.
That hardening is in progress elsewhere, not shipped here.

## Decision Drivers

- Pin 1 offline root.
- Constrain installation, purpose, and epoch.
- Preserve batch signing leases.

## Considered Options

### Issuance and Custody

| Option | Pros | Cons |
| --- | --- | --- |
| Direct offline-root issuance | Minimal delegation | Daily ceremonies |
| Offline root with local issuer | Automated epochs; offline root | Intermediate compromise risk |
| Online KMS/HSM/Vault root | Automated custody | Availability and algorithm constraints |

Cloud KMS is optional. HSM or Vault transit can operate locally.

### Replica Certificate

| Option | Pros | Cons |
| --- | --- | --- |
| Length-prefixed statement | V1 conventions; explicit scopes | New certificate fixtures |
| X.509 | Existing PKI tooling | Custom scopes and path validation |
| Canonical JSON | Readable fields | Another canonicalization profile |

## Decision

Propose an offline root delegating to a longer-lived, in-cluster intermediate issuer.
Bind organization, installation, allowed purposes, and a prohibition on further intermediate delegation.
Recommend annual intermediate renewal and 24-hour epoch rotation, without public connectivity.
Prefer local HSM or Vault transit; encrypted issuer storage needs owner approval.
Record issuance in [0001's registry](0001-per-replica-chains.md) and refuse duplicate `(UUID, epoch)` certification.
Reserve that pair and durably record its signed certificate digest before releasing the certificate.
Crashes may leave unused registrations, never usable unregistered certificates.

Choose v1-style length-prefixed statements, not JSON or X.509.
Use NUL-terminated domains, `U32LE` lengths, `U64LE` integers, raw keys, and absence markers.
Ed25519 signs SHA-256 of that preimage.
Bind issuer and subject fingerprints, organization, installation, purposes, and scope.
Freeze ordering and fixtures. Reject ambiguous key IDs and cross-purpose use.

### Signing Roles and Purposes

| Purpose | Authorized signer and scope |
| --- | --- |
| `certificate_issuance` | Intermediate; its installation and permitted child purposes |
| `replica_registry` | Intermediate; issuance, lease generations, replacements, losses |
| `receipt` | Epoch key; intents, outcomes, decisions, stream results |
| `control` | Epoch key; boundaries, mode changes, future sink-transition markers |
| `segment_descriptor` | Epoch key; descriptors for that same epoch |
| `legacy_segment` | Certified archival publisher; migrated v1 exports only |
| `inventory` | Certified singleton publisher; cross-replica inventories and head vectors |
| `checkpoint` | Separate certified log key; installation and origin |
| `trust_status` | Intermediate; signed status of its delegated keys and sequence cutoffs |
| `migration` | Offline root and final v1 key co-sign; neither signature may be delegated |

### Validity and Rotation

Epoch and sequence bounds govern validity.
Certify exactly 1 UUID and epoch, starting at sequence 1.
Its signed epoch-close fixes the inclusive terminal sequence.
Verifiers reject records beyond that bound.
Terminal coverage requires a signed inventory binding epoch-close and the final descriptor.
When Merkle mode is enabled, also prove inclusion of epoch-close against its checkpoint.
Unclosed epochs remain provisional prefixes.
Time windows guide renewal only; untrusted timestamps cannot establish cryptographic expiry.
Elapsed time alone cannot invalidate historical evidence.

Propose `agentgate keys rotate` at a batch boundary.
Keep admission open; bounded queues may briefly wait.
After the current batch, append epoch-close and durably seal the final segment descriptor.
Retain the old key until descriptor and directory fsync.
Durably activate the next certified epoch and write epoch-open at sequence 1 before queued work.
Inflight upstream calls continue.
Later outcomes reference earlier intents across epochs.
Epoch-close closes signing extent, not external-work completion.
Preserve lease-before-SQLite lock ordering; recover activation from durable phase metadata.
Current `Rotate(atSeq)` needs epoch-aware changes; calling `Rotate(1)` is not valid.

### Revocation and Offline Verification

Revocation policy remains an [owner decision](README.md#open-decisions).
Recommend intermediate-signed status every 6 hours; `next_update` is 24 hours after issuance.
Bind revision, predecessor digest, certificate IDs, and revoked epoch or sequence cutoffs.
Offline verification reports bundled status as-of time.
Freshness needs trusted verifier time or a separately pinned status revision.
Otherwise report freshness unknown, not unseen revocation checks.
Stale or missing status prevents current-trust success; historical checks report as-of status.
Revoke compromised intermediates through offline-root-signed replacement status.

Bundle certificates, registry history, status, descriptors, and applicable proofs or checkpoints.
Independently pin the root public key.
Raw-key v2 lifecycle tests do not require this hierarchy.
Legacy v1 retains its original trust rules outside a root-and-final-v1-key co-signed migration envelope.

## Consequences

### Positive

Root pinning supports automated local issuance.
Sequence bounds remain meaningful without trusted receipt clocks.

### Negative

The intermediate and registry require protected local state.
Revocation freshness remains a policy requirement.

### Risks and Mitigations

A compromised issuer can certify attacker keys. Restrict scope and audit issuance.
Cloned certificates remain usable. Require external fencing.
Coordinate manifest hardening without changing frozen v1 encoding.

## Implementation Notes

1. Freeze binary certificate fixtures and an offline root-pin procedure.
   Acceptance: wrong installation, purpose, epoch, or issuer delegation fails verification.
2. Rotate under continuous admission and inflight SSE, then crash at each boundary.
   Acceptance: no admission drain, exactly 1 epoch-open, and the old key survives until final descriptor durability.
3. Append records beyond epoch-close and remove its inventory coverage.
   Acceptance: out-of-bounds records fail; missing terminal coverage cannot pass as complete.
4. Test stale, missing, and revoked trust-status bundles without networking.
   Acceptance: report exact revocation scope and unknown freshness without fabricating clock-based trust.

## Related ADRs

- [0001: Identity and registry](0001-per-replica-chains.md).
- [0002: Cross-epoch outcomes](0002-two-phase-receipts.md).
- [0003: Descriptors](0003-durable-shipping.md).
- [0004: Publisher keys](0004-merkle-checkpoints.md).
