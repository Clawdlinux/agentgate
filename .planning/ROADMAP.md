# Roadmap: AgentGate Receipts and OSS Launch

## Overview

This milestone adds offline-verifiable action receipts to the existing gateway, then opens a clear contributor path.
The order is fixed: R1, R2, R3, R4, R5, R9, launch, R6, R7, R8, R10, R11.
Each phase corresponds to one PRD task and one future pull request.
Phase 1 blocks all later work.
Phase 6 is the OSS launch gate.
Phases 7 through 11 follow launch.

## Phases

- [x] **Phase 1: Apache 2.0 Release Basis** - Confirm authority and publish an accurate Apache 2.0 release basis. (completed 2026-08-13)
- [x] **Phase 2: Receipt Protocol** - Freeze deterministic, privacy-limited receipt bytes and fixtures. (completed 2026-08-13)
- [x] **Phase 3: Persistent Signing and Trust** - Establish durable Ed25519 identity, rotation, and public trust history. (completed 2026-08-14)
- [x] **Phase 4: Synchronous Request-Path Ledger** - Commit gap-free receipts for authenticated action outcomes before successful responses. (completed 2026-08-14)
- [x] **Phase 5: Independent Offline Verification** - Verify supplied receipt artifacts offline with pinned public trust. (completed 2026-08-14)
- [x] **Phase 6: 5-Minute Quickstart and OSS Launch** - Prove a new operator can produce and verify a receipt, then launch. (completed 2026-08-16)
- [x] **Phase 7: Signed Bounded Export** - Give auditors a bounded, verifier-compatible JSONL artifact. (completed 2026-08-16)
- [x] **Phase 8: Parent-Bound Biscuit Delegation** - Bind attenuated authority to verified requests and receipt lineage. (completed 2026-08-16)
- [x] **Phase 9: Google Workspace Featured Connector** - Add Gmail labels while keeping exactly 3 featured launch connectors. (completed 2026-08-16)
- [ ] **Phase 10: Sourced Product Comparison** - Publish qualified comparisons backed by current first-party evidence.
- [ ] **Phase 11: Contributor Entry Path** - Document contribution rules and open exactly 6 scoped starter issues.

## Phase Details

### Phase 1: Apache 2.0 Release Basis
**Goal**: Contributors can inspect a valid Apache 2.0 release basis after the owner confirms relicensing authority.
**Depends on**: Nothing
**PRD task**: R1
**Delivery**: One future pull request
**Requirements**: LIC-01, LIC-02, LIC-03, LIC-04
**Blocking gate**: Written owner authority must be recorded before R1 merges. Phase 1 blocks Phases 2 through 11.
**Success Criteria** (what must be TRUE):
  1. A release reviewer can confirm recorded authority for every distributed first-party file before R1 merges.
  2. A source checkout contains the unmodified Apache License 2.0 text and an accurate `NOTICE` file.
  3. A release scan finds Apache-compatible notices in first-party Go files and no Business Source License text.
  4. The README identifies Apache License 2.0 and links to the repository license.
**Plans**: TBD

### Phase 2: Receipt Protocol
**Goal**: Implementers and auditors share one deterministic receipt contract that omits sensitive request data.
**Depends on**: Phase 1
**PRD task**: R2
**Delivery**: One future pull request
**Requirements**: RCPT-01, RCPT-02, RCPT-03, RCPT-04, RCPT-05
**Success Criteria** (what must be TRUE):
  1. A receipt exposes the fixed R2 fields, types, order, limits, and versioned domain separation.
  2. Equivalent decoded parameter objects produce the same SHA-256 digest despite JSON whitespace or key order.
  3. Receipt artifacts contain no raw parameters, OAuth tokens, upstream bodies, or free-form provider errors.
  4. Two independent encoders produce identical canonical bytes for the same receipt.
  5. A checked-in golden fixture covers genesis linkage, Unicode, empty delegation, and integer boundaries.
**Plans**: TBD

### Phase 3: Persistent Signing and Trust
**Goal**: Operators retain a durable Ed25519 signer whose public history supports independent verification.
**Depends on**: Phase 2
**PRD task**: R3
**Delivery**: One future pull request
**Requirements**: KEY-01, KEY-02, KEY-03, KEY-04, KEY-05, KEY-06, KEY-07
**Success Criteria** (what must be TRUE):
  1. First persistent startup creates an Ed25519 keypair and stores its private material encrypted with a purpose-derived key.
  2. Restart preserves signer identity, while unreadable persistent key material stops startup without silent replacement.
  3. `GET /v1/receipts/pubkey` returns active and historical public keys with deterministic IDs and no private material.
  4. An operator can rotate keys while old receipts remain verifiable within explicit sequence intervals.
  5. An auditor can validate a receipt using only a separately trusted public root and public key history.
**Plans**: TBD

### Phase 4: Synchronous Request-Path Ledger
**Goal**: Authenticated action attempts produce gap-free committed receipts before AgentGate returns a successful response.
**Depends on**: Phase 3
**PRD task**: R4
**Delivery**: One future pull request
**Requirements**: LEDG-01, LEDG-02, LEDG-03, LEDG-04, LEDG-05, LEDG-06, LEDG-07, LEDG-08, LEDG-09, LEDG-10, LEDG-11
**Research**: Required. Resolve crash boundaries, durable outcomes, and composition-root ownership before planning.
**Success Criteria** (what must be TRUE):
  1. A populated database upgrades through `002_receipts.sql` without changing `audit_log` data.
  2. Production uses persistent SQLite auth, vault, migrations, scopes, and receipt dependencies before vault access.
  3. Every authenticated, schema-valid attempt reaches one receipt path for allowed, denied, limited, token, and upstream outcomes.
  4. Concurrent and restarted actions preserve committed sequence order without gaps, duplicates, or an in-memory source of truth.
  5. Failed receipt commits consume no sequence or success response. Measured p99 and docs exclude atomic SQLite plus SaaS guarantees.
**Plans**: TBD

### Phase 5: Independent Offline Verification
**Goal**: Auditors can verify supplied receipt chains offline without signer secrets or gateway state.
**Depends on**: Phase 4
**PRD task**: R5
**Delivery**: One future pull request
**Requirements**: VER-01, VER-02, VER-03, VER-04, VER-05, VER-06, VER-07, VER-08, VER-09, VER-10, VER-11
**Success Criteria** (what must be TRUE):
  1. The verifier reads SQLite, JSONL files, and JSONL standard input without private keys, gateway state, or network access.
  2. Verification requires a separately trusted public root and validates historical key transitions.
  3. Exit codes distinguish success, mismatches, and input failures while reporting the first failing sequence safely.
  4. Separate tests return mismatch status for modified, interior-deleted, inserted, and forged receipt records.
  5. Malformed inputs fail as input errors, and raw-chain completeness requires a trusted expected head.
**Plans**: TBD

### Phase 6: 5-Minute Quickstart and OSS Launch
**Goal**: A new operator can produce and verify a persistent GitHub receipt using the shipped Docker path.
**Depends on**: Phase 5
**PRD task**: R9
**Delivery**: One future pull request
**Requirements**: QST-01, QST-02, QST-03, QST-04, QST-05
**Launch gate**: OSS launch occurs only after all Phase 6 criteria pass. Phases 7 through 11 follow launch.
**Success Criteria** (what must be TRUE):
  1. The README begins with the verified-receipt quickstart before architecture or product detail.
  2. A clean checkout starts through `docker compose` without a host Go toolchain.
  3. The documented flow connects GitHub, performs one action, and verifies its SQLite receipt offline.
  4. Database and signing state survive a container restart during quickstart validation.
  5. A new operator completes every stated prerequisite and verification step in under 5 minutes.
**Plans**: TBD

### Phase 7: Signed Bounded Export (completed 2026-08-16)
**Goal**: An admin can give an auditor a bounded JSONL artifact with verifiable snapshot context.
**Depends on**: Phase 6 and the OSS launch
**PRD task**: R6
**Delivery**: One future pull request
**Requirements**: EXPT-01, EXPT-02, EXPT-03, EXPT-04, EXPT-05
**Success Criteria** (what must be TRUE):
  1. An authenticated admin can export inclusive sequence bounds through the documented HTTP endpoint.
  2. Export returns sequence-ordered JSONL from one SQLite snapshot and rejects invalid or oversized ranges.
  3. Export metadata binds requested bounds, actual bounds, count, anchor, keyset, and snapshot head.
  4. Full and partial exports verify offline through the same JSONL verifier.
  5. Exported artifacts contain no raw parameters, credentials, upstream bodies, or unrestricted provider errors.
**Plans**: TBD

### Phase 8: Parent-Bound Biscuit Delegation (completed 2026-08-16)
**Goal**: Agents can present attenuated authority that AgentGate verifies and binds to receipt lineage before dispatch.
**Depends on**: Phase 7
**PRD task**: R7
**Delivery**: One future pull request
**Requirements**: DELG-01, DELG-02, DELG-03, DELG-04, DELG-05, DELG-06
**Research**: Required. Recheck Biscuit releases, payload versions, issuance, root distribution, and draft revisions.
**Success Criteria** (what must be TRUE):
  1. AgentGate verifies each Biscuit token and request binding before registry, vault, or upstream access.
  2. Delegation checks bind agent, principal, service, action, limits, expiry, and trusted root.
  3. Receipts keep direct grants empty and store only ordered commitments for valid attenuated lineage.
  4. A chain-splicing test proves a grafted attenuation is rejected before dispatch.
  5. Package documentation cites both named drafts as design context without claiming standards compliance.
**Plans**: TBD

### Phase 9: Google Workspace Featured Connector (completed 2026-08-16)
**Goal**: Operators can use one narrow Gmail labels action through the existing registry and OAuth flow.
**Depends on**: Phase 8
**PRD task**: R8
**Delivery**: One future pull request
**Requirements**: CONN-01, CONN-02, CONN-03, CONN-04
**Success Criteria** (what must be TRUE):
  1. An operator can list Gmail labels through the existing service registry and OAuth flow.
  2. The connector requests the narrow Gmail labels scope and passes registry contract tests.
  3. Launch documentation features exactly GitHub, Slack, and Google Workspace.
  4. Stripe configuration and SDK support remain functional but unfeatured.
**Plans**: TBD

### Phase 10: Sourced Product Comparison
**Goal**: Readers can compare documented product capabilities without unsupported negatives or market claims.
**Depends on**: Phase 9
**PRD task**: R10
**Delivery**: One future pull request
**Requirements**: COMP-01, COMP-02, COMP-03, COMP-04, COMP-05
**Research**: Required. Refresh first-party competitor evidence and comparison wording before merge.
**Success Criteria** (what must be TRUE):
  1. `docs/comparison.md` contains the 5 required products and 5 required capability columns.
  2. Every positive competitor claim links to dated first-party public documentation.
  3. Missing public evidence appears as `Not documented`, never as a definitive absence.
  4. AgentGate claims link to shipped behavior and reproducible verification evidence.
  5. The comparison contains no unsupported benchmarks, scores, or absolute market claims.
**Plans**: TBD

### Phase 11: Contributor Entry Path
**Goal**: New contributors can prepare focused changes and choose from exactly 6 independently testable starter issues.
**Depends on**: Phase 10
**PRD task**: R11
**Delivery**: One future pull request
**Requirements**: OSS-01, OSS-02, OSS-03, OSS-04, OSS-05
**Success Criteria** (what must be TRUE):
  1. `CONTRIBUTING.md` explains prerequisites, build, test, lint, focused pull requests, and DCO sign-off.
  2. Contributor guidance states that `git commit -s` adds sign-off, not a cryptographic signature.
  3. Exactly 6 independently testable issues receive `good first issue` after the guidance lands.
  4. Starter work contains 4 service configuration tasks and 2 verifier output tasks.
  5. Every starter issue names files, acceptance checks, and a test path that needs no secrets.
**Plans**: TBD

## Progress

**Execution Order:** Phase 1 through Phase 11 in numeric order.

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Apache 2.0 Release Basis | 1/1 | Complete    | 2026-08-13 |
| 2. Receipt Protocol | 2/2 | Complete   | 2026-08-13 |
| 3. Persistent Signing and Trust | 1/1 | Complete | 2026-08-14 |
| 4. Synchronous Request-Path Ledger | 0/TBD | Not started | - |
| 5. Independent Offline Verification | 0/TBD | Not started | - |
| 6. 5-Minute Quickstart and OSS Launch | 0/TBD | Not started | - |
| 7. Signed Bounded Export | 0/TBD | Not started | - |
| 8. Parent-Bound Biscuit Delegation | 0/TBD | Not started | - |
| 9. Google Workspace Featured Connector | 0/TBD | Not started | - |
| 10. Sourced Product Comparison | 0/TBD | Not started | - |
| 11. Contributor Entry Path | 0/TBD | Not started | - |
