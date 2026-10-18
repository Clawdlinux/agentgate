# ADR-0005: Root-certified replica signing keys

## Status

Proposed

## Date

2026-10-18

## Context

The [signer store](../../internal/signer/store.go) caches decrypted Ed25519 keys in memory.
`WithActiveKey` holds a lease through each ledger transaction, excluding `Rotate` and `Close`.
`Rotate` switches keys at the next global sequence and clears old private bytes.
It creates neither epochs nor root certificates.

[V1 key history](../spec/RECEIPT-v1.md) uses independently trusted public keys with inclusive sequence windows.
Embedded export keys alone prove consistency, not organization identity.
Current [manifest verification](../../pkg/receiptspec/manifest.go) does not enforce its signer's validity window.
Manifest-signer validity-window hardening is in progress on another branch, not shipped in this baseline.

## Decision Drivers

- Let an offline verifier trust 1 independently provisioned organization root.
- Limit a compromised replica key's scope and lifetime.
- Keep roots outside request handling.
- Preserve cached signing and batch rotation leases.

## Considered Options

### Root Custody

| Option | Pros | Cons |
| --- | --- | --- |
| KMS or HSM | Managed custody or hardware protection | Product-specific algorithms, cost, and availability |
| Local Vault transit | Local automation and access policy | Additional service and recovery operations |
| Offline root ceremony | No online root dependency | Manual issuance and renewal planning |

Cloud KMS is optional. Local HSM, Vault transit, or an offline root must support air-gapped issuance.

### Replica Certificate

| Option | Pros | Cons |
| --- | --- | --- |
| Minimal signed statement | Small offline parser and explicit chain scopes | Team must specify encoding and constraints |
| X.509 | Existing tooling and enterprise PKI integration | Custom chain-scope extensions and path-validation complexity |
| Raw trusted replica keys | Matches v1 | Every verifier needs independently distributed key history |

## Decision

Propose an organization root held in an approved KMS, HSM, local Vault transit, or offline ceremony.
The initial proposed certificate profile uses Ed25519.
Reject incompatible backends instead of exporting root keys.
No request or offline verification requires reaching the root service.

Choose a minimal, versioned signed certificate statement over X.509.
Propose canonical JSON, restricted integers, and a dedicated signature domain.
Gateway params canonicalization does not define certificate semantics.
The root signs the statement digest.
Bind organization ID, issuer fingerprint, subject public key, key ID, allowed purpose, and validity timestamps.
Replica certificates also bind exactly 1 replica UUID and epoch.
Log certificates instead bind a checkpoint origin and log-signing purpose.
Freeze bytes, timestamp units, and domains before implementation.
Reject duplicate or ambiguous key IDs.

Propose a default replica key lifetime of 24 hours, configurable for offline renewal ceremonies.
Offline installations must provision renewal material or approve longer intervals.
Expiry stops new signing. Never silently extend trust or fall back to cloud services.
Keep private replica keys encrypted at rest and cached under the current lease model.
Retain certificates after private-key destruction.

Propose `agentgate keys rotate` as a new command, not an existing CLI feature.
Stop admission, resolve inflight attempts, then close and seal the authorized epoch.
Atomically persist the next certified key and epoch before resuming.
Preserve lease-before-SQLite lock ordering.
Current `Rotate(atSeq)` uses global windows; v2 needs an epoch-aware transaction, not `Rotate(1)`.
Interrupted rotation must resume at exactly 1 committed epoch boundary.

Offline verification needs only receipts, proofs, checkpoint, and an independently pinned root public key.
Bundles include certificates, descriptors, and signed trust-status statements.
Verify purpose, chain scope, key bytes, and applicable sequence or timestamp windows.
Do not reject historical receipts merely because their certificate is expired today.
Receipt timestamps alone do not prove signing time.
Without independently trusted time, report backdating limitations.
Offline revocation reflects bundled signed status, not unseen later revocations.
Legacy v1 exports still require their original trusted key history unless a reviewed root-signed migration descriptor binds it.

## Consequences

### Positive

Root pinning replaces manual trust distribution for each new replica key.
Purpose and epoch constraints reduce cross-role key misuse.

### Negative

Certificate issuance and root recovery require operational procedures.
Offline installations trade short lifetimes against renewal workload.

### Risks and Mitigations

A compromised root can certify attacker keys. Require protected custody and an audited root replacement procedure.
Compromised replicas can backdate receipts. Document clock limits and compare independently retained checkpoints.
Manifest hardening may change verifier rejection cases. Coordinate that branch without changing frozen v1 encoding.

## Implementation Notes

1. Freeze certificate fixtures and an offline root-pin procedure.
   Acceptance: tampered scope, purpose, validity, or public key fails verification.
2. Integrate epoch-aware activation with existing cached-key leases.
   Acceptance: rotation during a batch waits; no batch uses a cleared or wrong-epoch key.
3. Crash-test each rotation boundary.
   Acceptance: restart selects exactly 1 active epoch and never emits uncertified receipts.
4. Test root outages, expiration, offline renewal, and archived evidence.
   Acceptance: new signing stops on expiry; historical verification works without any network.

## Related ADRs

- [0001: Epoch identity](0001-per-replica-chains.md) scopes replica certificates.
- [0002: Recovery](0002-two-phase-receipts.md) resolves inflight attempts before rotation.
- [0003: Segments](0003-durable-shipping.md) carries public certificate history.
- [0004: Checkpoints](0004-merkle-checkpoints.md) uses a separately certified log purpose.
