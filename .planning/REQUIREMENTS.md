# Requirements: AgentGate Receipts and OSS Launch

**Defined:** 2026-08-11
**Core Value:** Every agent action produces evidence an independent auditor can verify offline without AgentGate's secret key.

## Milestone Order

Deliver tasks in this order:

`R1 -> R2 -> R3 -> R4 -> R5 -> R9 -> launch -> R6 -> R7 -> R8 -> R10 -> R11`

Each task uses one pull request with signed-off commits.

## v1 Requirements

### Apache Release Basis

- [x] **LIC-01**: The owner records authority to relicense every distributed first-party file before R1 merges.
- [x] **LIC-02**: A source checkout contains the unmodified Apache License 2.0 text and an accurate `NOTICE` file.
- [x] **LIC-03**: All first-party Go files use Apache-compatible notices and contain no Business Source License text.
- [x] **LIC-04**: The README identifies Apache License 2.0 and links to the repository license.

### Receipt Protocol

- [x] **RCPT-01**: A receipt contains every R2 field with fixed types, ordering, limits, and versioned domain separation.
- [x] **RCPT-02**: Equivalent decoded parameter objects produce the same SHA-256 digest regardless of JSON whitespace or key order.
- [x] **RCPT-03**: Receipts persist no raw parameters, OAuth tokens, upstream bodies, or free-form provider errors.
- [x] **RCPT-04**: Independent encoders produce identical canonical bytes for the same fixed receipt.
- [x] **RCPT-05**: A checked-in golden fixture covers genesis linkage, Unicode, empty delegation, and integer boundaries.

### Signing And Trust

- [ ] **KEY-01**: First persistent startup generates one Ed25519 keypair from cryptographic randomness.
- [ ] **KEY-02**: The private signing key persists encrypted under a purpose-derived key from the existing vault master key.
- [ ] **KEY-03**: Restart preserves the signer identity and never replaces an unreadable persistent key silently.
- [ ] **KEY-04**: `GET /v1/receipts/pubkey` publishes active and historical public keys without private material.
- [ ] **KEY-05**: Each public key has a deterministic key ID bound to its Ed25519 public bytes.
- [ ] **KEY-06**: Key rotation preserves verification of old receipts and binds each key to its valid sequence interval.
- [ ] **KEY-07**: An independent verifier validates a receipt using only a separately trusted public root and public key history.

### Request-Path Ledger

- [ ] **LEDG-01**: Migration `002_receipts.sql` adds receipt storage without changing or deleting `audit_log` data.
- [ ] **LEDG-02**: The production command uses persistent SQLite auth, vault, migrations, and receipt dependencies.
- [ ] **LEDG-03**: Verified key scopes bind each receipted agent and human principal before vault access.
- [ ] **LEDG-04**: Every authenticated, schema-valid action attempt reaches one centralized receipt outcome path.
- [ ] **LEDG-05**: Allowed, denied, rate-limited, token-failed, upstream-failed, and upstream-returned outcomes receive receipts.
- [ ] **LEDG-06**: Sequence allocation, predecessor lookup, signing, insertion, and commit occur as one serialized transaction.
- [ ] **LEDG-07**: A failed receipt transaction consumes no sequence and returns no successful action response.
- [ ] **LEDG-08**: Restart resumes from the committed head without gaps, duplicates, or an in-memory source of truth.
- [ ] **LEDG-09**: One hundred concurrent actions produce a chain containing exactly sequences 1 through 100.
- [ ] **LEDG-10**: R4 records added p99 receipt latency and revisits design only when overhead exceeds 50 ms.
- [ ] **LEDG-11**: Documentation limits crash guarantees across SQLite and external SaaS side effects.

### Offline Verification

- [ ] **VER-01**: The verifier reads receipt chains from SQLite, JSONL files, and JSONL standard input.
- [ ] **VER-02**: Verification needs no private key, signer state, gateway process, or network connection.
- [ ] **VER-03**: The verifier requires a separately trusted public root and validates historical key transitions.
- [ ] **VER-04**: Exit code 0 means all requested checks passed, 1 means mismatch, and 2 means input failure.
- [ ] **VER-05**: A dedicated test proves a modified receipt fails verification with exit code 1.
- [ ] **VER-06**: A dedicated test proves an interior-deleted receipt fails verification with exit code 1.
- [ ] **VER-07**: A dedicated test proves an inserted receipt fails verification with exit code 1.
- [ ] **VER-08**: A dedicated test proves a forged Ed25519 signature fails verification with exit code 1.
- [ ] **VER-09**: Malformed input, unknown key IDs, unsupported versions, and empty sources fail with exit code 2.
- [ ] **VER-10**: Verification reports the first failing sequence without printing sensitive receipt fields.
- [ ] **VER-11**: Raw-chain completeness claims require a trusted expected head and distinguish partial ranges.

### Five-Minute Quickstart

- [ ] **QST-01**: The README starts with the verified-receipt quickstart before architecture or product detail.
- [ ] **QST-02**: A clean checkout starts AgentGate through `docker compose` without a host Go toolchain.
- [ ] **QST-03**: The documented flow connects GitHub, performs one `/v1/act`, and verifies its SQLite receipt offline.
- [ ] **QST-04**: Persistent database and signing state survive a container restart during the quickstart validation.
- [ ] **QST-05**: A new operator completes every stated prerequisite and verification step in under five minutes.

### Receipt Export

- [ ] **EXPT-01**: An admin can export inclusive sequence bounds through `GET /v1/receipts/export?from=&to=`.
- [ ] **EXPT-02**: Export returns sequence-ordered JSONL from one SQLite snapshot with bounded range validation.
- [ ] **EXPT-03**: Export metadata binds requested bounds, actual bounds, count, anchor, keyset, and snapshot head.
- [ ] **EXPT-04**: Full and partial exports verify offline using the same JSONL verifier.
- [ ] **EXPT-05**: Export contains no raw parameters, credentials, upstream bodies, or unrestricted provider errors.

### Attenuated Delegation

- [ ] **DELG-01**: AgentGate verifies each Biscuit token and request binding before registry, vault, or upstream access.
- [ ] **DELG-02**: Delegation checks bind the verified agent, human principal, service, action, limits, expiry, and root.
- [ ] **DELG-03**: Receipts store ordered commitments derived from verified Biscuit blocks, never raw tokens or policy source.
- [ ] **DELG-04**: Direct grants keep `delegation_chain` empty, while valid attenuated grants preserve their ordered lineage.
- [ ] **DELG-05**: A chain-splicing test grafts chain A into chain B and proves AgentGate rejects it before dispatch.
- [ ] **DELG-06**: Package documentation cites both named IETF drafts as design context without claiming wire compatibility.

### Google Workspace Connector

- [ ] **CONN-01**: Google Workspace provides one Gmail labels action through the existing service registry and OAuth flow.
- [ ] **CONN-02**: The connector requests the narrow Gmail labels scope and has registry contract tests.
- [ ] **CONN-03**: Launch documentation features GitHub, Slack, and Google Workspace as exactly three connectors.
- [ ] **CONN-04**: Stripe configuration and SDK support remain functional but are not featured at launch.

### Sourced Comparison

- [ ] **COMP-01**: `docs/comparison.md` contains the five required products and five required capability columns.
- [ ] **COMP-02**: Every positive competitor claim links to dated first-party public documentation.
- [ ] **COMP-03**: Missing public evidence is labeled `Not documented`, never inferred as a definitive absence.
- [ ] **COMP-04**: AgentGate comparison claims link to shipped behavior and reproducible verification evidence.
- [ ] **COMP-05**: The comparison contains no unsupported benchmarks, scores, or absolute market claims.

### Contribution Path

- [ ] **OSS-01**: Root `CONTRIBUTING.md` documents prerequisites, build, test, lint, focused pull requests, and DCO sign-off.
- [ ] **OSS-02**: Contributor guidance explains that `git commit -s` adds a sign-off and is not cryptographic signing.
- [ ] **OSS-03**: Exactly six independently testable issues receive the `good first issue` label after guidance lands.
- [ ] **OSS-04**: Starter work comprises four service configurations and two verifier-output formatting issues.
- [ ] **OSS-05**: Every starter issue names files, acceptance checks, and a test path requiring no secrets.

## v2 Requirements

### External Checkpoints

- **ANCH-01**: An auditor can compare receipt heads against independently published Rekor or Sigstore checkpoints.

### Storage Backends

- **STORE-01**: An operator can store receipts in PostgreSQL without changing canonical bytes or verifier results.

### Consent Mapping

- **DPDP-01**: A DPO can map released receipt fields to DPDP consent-artifact obligations.

## Out of Scope

| Feature | Reason |
|---------|--------|
| New delegation token specification | Existing IETF work already covers the design space. |
| Web UI or dashboard | CLI and HTTP paths prove the milestone value. |
| More than three featured connectors | Connector breadth is not AgentGate's receipt wedge. |
| Kubernetes, Helm, or gVisor | Docker Compose minimizes adoption friction. |
| Multi-tenant isolation | The current milestone remains single tenant. |
| Receipt repair | Repair would hide evidence of tampering. |
| Hosted key discovery during verification | Offline trust requires pinned local inputs. |
| ANF performance claims | Those measurements belong to another product. |
| Unsupported competitor negatives | Missing documentation does not prove absence. |

## Traceability

| Requirement | Phase | PRD task | Status |
|-------------|-------|----------|--------|
| LIC-01 | Phase 1 | R1 | Complete |
| LIC-02 | Phase 1 | R1 | Complete |
| LIC-03 | Phase 1 | R1 | Complete |
| LIC-04 | Phase 1 | R1 | Complete |
| RCPT-01 | Phase 2 | R2 | Complete |
| RCPT-02 | Phase 2 | R2 | Complete |
| RCPT-03 | Phase 2 | R2 | Complete |
| RCPT-04 | Phase 2 | R2 | Complete |
| RCPT-05 | Phase 2 | R2 | Complete |
| KEY-01 | Phase 3 | R3 | Pending |
| KEY-02 | Phase 3 | R3 | Pending |
| KEY-03 | Phase 3 | R3 | Pending |
| KEY-04 | Phase 3 | R3 | Pending |
| KEY-05 | Phase 3 | R3 | Pending |
| KEY-06 | Phase 3 | R3 | Pending |
| KEY-07 | Phase 3 | R3 | Pending |
| LEDG-01 | Phase 4 | R4 | Pending |
| LEDG-02 | Phase 4 | R4 | Pending |
| LEDG-03 | Phase 4 | R4 | Pending |
| LEDG-04 | Phase 4 | R4 | Pending |
| LEDG-05 | Phase 4 | R4 | Pending |
| LEDG-06 | Phase 4 | R4 | Pending |
| LEDG-07 | Phase 4 | R4 | Pending |
| LEDG-08 | Phase 4 | R4 | Pending |
| LEDG-09 | Phase 4 | R4 | Pending |
| LEDG-10 | Phase 4 | R4 | Pending |
| LEDG-11 | Phase 4 | R4 | Pending |
| VER-01 | Phase 5 | R5 | Pending |
| VER-02 | Phase 5 | R5 | Pending |
| VER-03 | Phase 5 | R5 | Pending |
| VER-04 | Phase 5 | R5 | Pending |
| VER-05 | Phase 5 | R5 | Pending |
| VER-06 | Phase 5 | R5 | Pending |
| VER-07 | Phase 5 | R5 | Pending |
| VER-08 | Phase 5 | R5 | Pending |
| VER-09 | Phase 5 | R5 | Pending |
| VER-10 | Phase 5 | R5 | Pending |
| VER-11 | Phase 5 | R5 | Pending |
| QST-01 | Phase 6 | R9 | Pending |
| QST-02 | Phase 6 | R9 | Pending |
| QST-03 | Phase 6 | R9 | Pending |
| QST-04 | Phase 6 | R9 | Pending |
| QST-05 | Phase 6 | R9 | Pending |
| EXPT-01 | Phase 7 | R6 | Pending |
| EXPT-02 | Phase 7 | R6 | Pending |
| EXPT-03 | Phase 7 | R6 | Pending |
| EXPT-04 | Phase 7 | R6 | Pending |
| EXPT-05 | Phase 7 | R6 | Pending |
| DELG-01 | Phase 8 | R7 | Pending |
| DELG-02 | Phase 8 | R7 | Pending |
| DELG-03 | Phase 8 | R7 | Pending |
| DELG-04 | Phase 8 | R7 | Pending |
| DELG-05 | Phase 8 | R7 | Pending |
| DELG-06 | Phase 8 | R7 | Pending |
| CONN-01 | Phase 9 | R8 | Pending |
| CONN-02 | Phase 9 | R8 | Pending |
| CONN-03 | Phase 9 | R8 | Pending |
| CONN-04 | Phase 9 | R8 | Pending |
| COMP-01 | Phase 10 | R10 | Pending |
| COMP-02 | Phase 10 | R10 | Pending |
| COMP-03 | Phase 10 | R10 | Pending |
| COMP-04 | Phase 10 | R10 | Pending |
| COMP-05 | Phase 10 | R10 | Pending |
| OSS-01 | Phase 11 | R11 | Pending |
| OSS-02 | Phase 11 | R11 | Pending |
| OSS-03 | Phase 11 | R11 | Pending |
| OSS-04 | Phase 11 | R11 | Pending |
| OSS-05 | Phase 11 | R11 | Pending |

**Coverage:**
- v1 requirements: 68 total
- Mapped to task groups: 68
- Unmapped: 0

---
*Requirements defined: 2026-08-11*
*Last updated: 2026-08-11 after roadmap creation*
