# Project Research Summary

**Project:** AgentGate Receipts and OSS Launch
**Domain:** Signed agent-action receipts and open source launch
**Researched:** 2026-08-11
**Confidence:** HIGH for the P0 roadmap. MEDIUM for delegation, legal authority, and market absence claims.

## Executive Summary

AgentGate is a brownfield Go gateway that brokers agent calls to SaaS APIs without exposing OAuth tokens.
This milestone adds gateway-attested receipts that an auditor can verify offline using separately trusted public keys.
Experts build this as a small protocol, a synchronous SQLite ledger, and a strict standalone verifier.

The release order is fixed: R1, R2, R3, R4, R5, R9, launch, then R6 through R11.
Keep the existing gateway, raw SQL, SQLite, and Docker Compose path.
Use Ed25519, fixed binary canonical encoding, hashed parameters, persistent key history, and fail-closed receipt writes.
Do not extend the unused buffered audit logger or add a second storage backend.

The largest risk sits at R4 because SQLite cannot transact with an external SaaS side effect.
P0 can guarantee gap-free committed receipts and response-after-commit behavior.
It cannot guarantee exactly-once evidence across process failure.
Other launch gates are copyright authority, trusted key bootstrap, strict canonicalization, qualified completeness claims, and a cold quickstart.

## Key Findings

### Recommended Stack

Retain the current Go module line and existing package structure.
The standard library covers receipt cryptography, encoding, key serialization, JWK output, and JSONL verification.
SQLite remains the single-tenant ledger, with one synchronous writer and explicit immediate transactions.

**Core technologies:**

- **Go `1.25` module line:** Keep the current build contract and use patched release toolchains.
- **`crypto/ed25519`:** Sign entry hashes with public verification and no shared forging secret.
- **`crypto/sha256`:** Commit parameters, receipt entries, and public-key thumbprints.
- **Fixed binary encoding:** Use explicit little-endian widths and length-prefixed UTF-8 strings.
- **`encoding/json` v1:** Decode parameters with `UseNumber`, normalize once, then hash the resulting bytes.
- **AES-256-GCM plus HKDF:** Encrypt PKCS #8 receipt keys under a purpose-derived wrapping key.
- **RFC 7517 JWK Set:** Publish active and retired public keys with RFC 7638 thumbprint identifiers.
- **SQLite and `database/sql`:** Store keys and receipts through additive raw SQL migrations.
- **`go-sqlite3` `v1.14.49`:** Upgrade before ledger work and retain the current driver family.
- **Biscuit Go `v2.2.0`:** Consider only during R7 after a fresh compatibility and security check.
- **Docker Compose:** Keep the clean-machine adoption path and require no local Go toolchain.

Do not add JOSE, CBOR, Protobuf, JCS, KMS, or release-framework dependencies for P0 receipts.
Do not change the Go directive during this milestone.

### Expected Features

**Must have before launch:**

- **R1 Apache 2.0 basis:** Add `LICENSE`, `NOTICE`, source headers, and accurate README license text.
- **R2 deterministic receipt:** Freeze versioned bytes and persist only a deterministic parameter digest.
- **R3 public-key signing:** Persist encrypted Ed25519 keys, key identifiers, rotations, and public history.
- **R4 gap-free ledger:** Append one receipt synchronously for each admitted action outcome.
- **R5 offline verifier:** Verify SQLite, JSONL, and stdin with stable exit codes and no network.
- **R9 cold quickstart:** Reach a verified GitHub receipt through shipped Docker tools in under 5 minutes.

**Ship after launch in this exact order:**

- **R6 bounded export:** Produce an authenticated, sequence-ordered JSONL artifact from one SQLite snapshot.
- **R7 delegated authority:** Verify parent-bound Biscuit attenuation and receipt only derived lineage identifiers.
- **R8 Google Workspace:** Feature one Gmail labels action while retaining Stripe support.
- **R10 sourced comparison:** Compare documented feature presence using dated first-party sources.
- **R11 contribution path:** Land contributor guidance before opening exactly 6 scoped starter issues.

**Defer beyond this milestone:**

- **External checkpoints:** Rekor or Sigstore anchoring remains P2.
- **PostgreSQL receipts:** SQLite remains the only receipt backend.
- **DPDP mapping:** Map only a released, stable receipt format.
- **Other product surfaces:** No dashboard, hosted verifier, Kubernetes packaging, or multi-tenant isolation.

### Architecture Approach

The production composition root must open one persistent SQLite database and inject real runtime dependencies.
The gateway action handler owns outcome collection.
A narrow receipt recorder owns ordered signing and persistence.
The verifier remains independent from server state and network access.

**Major components:**

1. **Composition root:** Open SQLite, migrate, validate keys, and construct production dependencies.
2. **Action handler:** Authenticate, normalize parameters, authorize, dispatch upstream, and buffer the final response.
3. **Receipt protocol:** Define v1 fields, canonical bytes, digests, signatures, and golden fixtures.
4. **Key manager:** Generate, wrap, persist, rotate, and publish receipt signing keys.
5. **SQLite ledger:** Allocate sequence, read the head, sign, insert, and commit atomically.
6. **Receipt HTTP handler:** Publish key history and later serve authenticated bounded exports.
7. **Offline verifier:** Parse strict inputs and validate trust, order, hashes, links, signatures, and bounds.
8. **Verification command:** Adapt SQLite, files, and stdin while preserving exit codes.
9. **Delegation policy:** Verify Biscuit authority before vault access and return derived lineage identifiers.

**Architecture choices:**

- Attach receipts to `gateway.handleAct`, which owns the current upstream call path.
- Preserve `audit_log`; the buffered audit logger stays separate from receipt correctness.
- Use the STACK v1 fixed binary construction as the R2 starting specification.
- Include the format version, previous hash, and signer identifier inside the entry commitment.
- Treat the PRD `error` field as a bounded error code, never raw error text.
- Derive principals and agent identifiers from verified scoped key records.
- Receipt authenticated, syntactically valid action attempts after identity resolution.
- Keep malformed and unauthenticated traffic in bounded security logs.
- Buffer HTTP outcomes and write them only after the receipt transaction commits.
- Use `_txlock=immediate`, WAL, `_synchronous=FULL`, and one receipt-writer connection.
- Require a separately pinned JWK Set or initial root key for offline trust.
- Preserve historical public keys and bind rotations to sequence ranges.
- Use explicit expected heads for raw-chain completeness checks.
- Add a signed range manifest during R6 for portable bounded exports.
- Keep external checkpointing outside this milestone.

### Critical Pitfalls

1. **Unproven relicense authority:** Confirm ownership and imported-code provenance before replacing BSL notices.
2. **Caller-controlled principal:** Wire scoped database authentication before signing any human attribution.
3. **Split transaction domains:** Never claim SQLite and an external SaaS action commit atomically.
4. **Self-authenticating keys:** Require a pinned trust input and verify historical key transitions.
5. **False completeness:** A chain alone cannot reveal an unknown deleted suffix or hidden fork.
6. **Ambiguous bytes:** Freeze strict parsing, versioned domain separation, numeric rules, and golden vectors in R2.
7. **Shared signer bug:** Test the verifier with independent fixtures and malformed-input corpora.
8. **Missed denial paths:** Centralize every admitted outcome before any response write.
9. **Ephemeral quickstart:** Fix database, key, environment, registry, and restart wiring before timing R9.
10. **Overstated claims:** Say `gateway-attested`, `tamper-evident`, and `verifies the supplied artifact`.

Avoid `tamper-proof`, `complete`, `non-repudiable`, `anonymous`, and `replayable` in launch copy.

## Implications for Roadmap

The roadmap should use 11 ordered phases.
Each phase maps to one PRD task and one pull request.
R2 through R5 remain one product block, but they do not become one pull request.

### Phase 1: R1 Apache 2.0 Release Basis

**Rationale:** Contribution work cannot start before legal distribution rights are clear.

**Delivers:** Apache 2.0 `LICENSE`, accurate `NOTICE`, header replacement, and README license text.

**Gate:** Obtain written owner authority and classify imported, generated, and attributed files.

**Avoids:** Publishing an open source label without proven relicensing authority.

### Phase 2: R2 Receipt Protocol

**Rationale:** Storage, signing, and verification all depend on one frozen byte contract.

**Delivers:** Receipt types, `agentgate-receipt-v1`, parameter normalization, hashes, strict limits, and golden vectors.

**Uses:** Standard-library SHA-256, fixed widths, length prefixes, UTF-8 validation, and typed JSON numbers.

**Avoids:** JSON-signing ambiguity, raw parameter leakage, unstable errors, and protocol confusion.

**Boundary:** Keep the PRD field set. Any new semantic field needs an explicit PRD amendment.

### Phase 3: R3 Persistent Signing and Trust

**Rationale:** Independent verification requires stable signer identity before the request path emits receipts.

**Delivers:** Ed25519 signing, encrypted PKCS #8 custody, JWK history, rotation, and restart continuity.

**Uses:** RFC 7638 key identifiers, RFC 9864 `Ed25519`, HKDF, and AES-256-GCM.

**Implements:** Persistence prerequisites must land here, even though R4 owns request-path ledger wiring.

**Avoids:** Temporary keys, key substitution, lost history, and silent signer regeneration.

### Phase 4: R4 Synchronous Request-Path Ledger

**Rationale:** The current production command and handler do not use persistent auth, vault, limits, or audit storage.

**Delivers:** Transactional migrations, persistent composition, scoped auth, rate limiting, ledger append, and outcome buffering.

**Uses:** One SQLite writer, `BEGIN IMMEDIATE`, WAL, full synchronization, and bounded append contexts.

**Acceptance:** Verify 100 concurrent calls, restart at sequence 101, and reuse rolled-back sequence numbers.

**Measure:** Record added p99 write latency. Revisit only when overhead exceeds 50 ms.

**Avoids:** Sequence forks, dropped receipts, early response writes, missed denials, and false principal claims.

**Limit:** Do not claim exactly-once evidence when a process fails after an upstream side effect.

### Phase 5: R5 Independent Offline Verifier

**Rationale:** The receipt claim is incomplete until a separate tool verifies artifacts without signer state.

**Delivers:** SQLite, JSONL, and stdin adapters; strict parsing; pinned trust; and exit codes `0`, `1`, and `2`.

**Acceptance:** Detect modified, interior-deleted, inserted, forged, reordered, unknown-key, and malformed records.

**Completeness:** Require an expected head to detect final-suffix deletion in raw logs.

**Avoids:** Network trust discovery, permissive parsing, silent repair, and misleading segment success.

### Phase 6: R9 Cold Quickstart and OSS Launch

**Rationale:** Launch only after a new operator can produce and verify the P0 artifact.

**Delivers:** A top-level GitHub quickstart using `docker compose`, OAuth linking, one action, and offline verification.

**Acceptance:** Use empty Docker caches, no host Go toolchain, blocked verifier networking, and a container restart.

**Gate:** Time the full documented path with a new operator and include every stated prerequisite.

**Avoids:** Warm-cache timing, hidden setup, missing service files, ephemeral state, and premature launch claims.

### Phase 7: R6 Signed Bounded Export

**Rationale:** Auditors need a portable artifact after the core verifier is stable.

**Delivers:** Admin-authenticated inclusive ranges, one-snapshot JSONL, signed bounds, counts, and head metadata.

**Acceptance:** Verify full and partial ranges, rejected bounds, truncation, authorization, and privacy canaries.

**Avoids:** Bare partial chains, inconsistent live reads, unlimited exports, and WAL-copy mistakes.

### Phase 8: R7 Parent-Bound Biscuit Delegation

**Rationale:** Delegation evidence depends on the stable receipt path, export shape, and verifier.

**Delivers:** Pre-vault Biscuit verification, attenuation checks, derived block identifiers, and signed denial receipts.

**Gate:** Recheck tagged Go support, signature payload versions, security status, and issuance assumptions.

**Acceptance:** Reject wrong roots, principals, agents, widened scopes, expired chains, excessive depth, and grafted blocks.

**Avoids:** Raw token storage, free-form lineage, local cryptography, and false IETF compliance claims.

### Phase 9: R8 Google Workspace Featured Connector

**Rationale:** The featured demo needs a common, low-risk account after delegation work lands.

**Delivers:** One Gmail-backed config for `users.labels.list` with the narrow `gmail.labels` scope.

**Boundary:** Feature GitHub, Slack, and Google Workspace. Keep Stripe working but unfeatured.

**Avoids:** Connector-framework changes, multi-API bundling, restricted scopes, and connector breadth work.

### Phase 10: R10 Sourced Comparison

**Rationale:** Comparison claims must describe shipped behavior and the final featured connector set.

**Delivers:** A dated feature-presence table for Nango, Composio, Astrix, Oasis, and AgentGate.

**Gate:** Refresh every first-party source immediately before merge and publication.

**Wording:** Use `Not documented` when public evidence is missing. Never infer a definitive `No`.

**Avoids:** Absolute market claims, stale negatives, unsupported benchmarks, and mismatched launch copy.

### Phase 11: R11 Contribution Path

**Rationale:** Starter issues should appear only after terms, setup, and acceptance guidance are public.

**Delivers:** Root `CONTRIBUTING.md`, DCO sign-off guidance, and exactly 6 independently testable issues.

**Issue split:** Create 4 service-config tasks and 2 verifier-output tasks.

**Avoids:** Calling `git commit -s` a cryptographic signature or opening vague starter work.

### Phase Ordering Rationale

- R1 legally enables contribution before technical outreach.
- R2 freezes semantics before signatures, persistence, or parsers depend on them.
- R3 establishes stable signer trust before R4 emits production receipts.
- R4 creates the authoritative chain before R5 judges it.
- R5 makes R9's end-to-end claim independently testable.
- R9 is the final launch gate. P1 work must not delay it.
- R6 through R11 follow the exact PRD sequence after launch.
- P2 work does not enter any R1 through R11 pull request.

### Research Flags

Phases needing deeper research during planning:

- **Phase 4, R4:** Resolve crash boundaries, durable outcome semantics, and current composition-root ownership.
- **Phase 8, R7:** Recheck Biscuit releases, payload versions, issuance, root distribution, and draft revisions.
- **Phase 10, R10:** Repeat time-sensitive competitor research using first-party sources.

Phases needing a non-research gate:

- **Phase 1, R1:** Obtain written legal authority before implementation.
- **Phase 6, R9:** Run a cold prototype and timed operator test before publishing the claim.

Phases with established patterns that can skip research-phase:

- **Phase 2, R2:** The cryptographic primitives and byte-format requirements are well documented.
- **Phase 3, R3:** Standard Ed25519, JWK, PKCS #8, HKDF, and AES-GCM patterns apply.
- **Phase 5, R5:** Strict streaming verification and exit semantics are already defined.
- **Phase 7, R6:** SQLite snapshot reads and signed range metadata have clear patterns.
- **Phase 9, R8:** Gmail labels and OAuth scopes have official documentation.
- **Phase 11, R11:** GitHub contribution guidance and DCO semantics are stable.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Official Go, RFC, SQLite, Docker, and GitHub sources support P0 choices. |
| Features | HIGH | The PRD fixes launch scope, priorities, and delivery order. |
| Architecture | HIGH | Current source tracing identifies the handler and missing production wiring. |
| Pitfalls | HIGH | Most risks follow directly from source behavior and cryptographic trust rules. |
| Biscuit delegation | MEDIUM | Stable Go support exists, but v3 and third-party block support remain unsettled. |
| Legal authority | LOW | Repository history cannot prove copyright ownership or employer consent. |
| Competitor absence claims | LOW | Missing public documentation cannot prove a feature does not exist. |

**Overall confidence:** HIGH for roadmap structure. MEDIUM for R7 details and external launch claims.

### Gaps to Address

- **Relicense authority:** Obtain written confirmation before R1 merges.
- **Receipt golden contract:** Check in fixed bytes before R3 starts.
- **Cross-language evidence:** Validate the golden receipt with an independent encoder or fixture generator.
- **Crash semantics:** Define the supported failure claim before changing R4's request flow.
- **Trust bootstrap:** Freeze the pinned key input and CLI flag contract across R3 and R5.
- **Tail deletion:** Require a trusted expected head now; external checkpointing remains P2.
- **Snapshot forks:** State that local verification cannot detect unseen alternative histories.
- **Biscuit support:** Recheck the stable module path and supported format at R7 planning time.
- **Quickstart wiring:** Fix persistent storage, environment names, service config paths, and packaged verifier access.
- **Launch language:** Replace the PRD's `replayable` and absolute competitor claims with qualified wording.

## Sources

### Primary: High Confidence

- [PROJECT.md](../PROJECT.md): Active requirements, constraints, and milestone decisions.
- [PRD-receipts-oss.md](../../PRD-receipts-oss.md): Locked scope, task order, acceptance cases, and launch boundary.
- [STACK.md](STACK.md): Versions, cryptographic formats, storage settings, and release tooling research.
- [FEATURES.md](FEATURES.md): P0, P1, P2, anti-features, and dependency order.
- [ARCHITECTURE.md](ARCHITECTURE.md): Current source tracing, component boundaries, and request flow.
- [PITFALLS.md](PITFALLS.md): Failure modes, claim limits, phase gates, and evidence requirements.
- [Go `crypto/ed25519`](https://pkg.go.dev/crypto/ed25519): Signing API and fixed key sizes.
- [RFC 8032](https://www.rfc-editor.org/rfc/rfc8032.html): Ed25519 behavior and context guidance.
- [RFC 7517](https://www.rfc-editor.org/rfc/rfc7517.html): JWK Set format.
- [RFC 7638](https://www.rfc-editor.org/rfc/rfc7638.html): Content-derived key identifiers.
- [RFC 9864](https://www.rfc-editor.org/rfc/rfc9864.html): Current `Ed25519` JOSE algorithm naming.
- [SQLite transactions](https://sqlite.org/lang_transaction.html): Immediate transactions and writer behavior.
- [SQLite WAL](https://www.sqlite.org/wal.html): WAL concurrency and snapshot caveats.
- [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0): License text and distribution terms.
- [Gmail labels API](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.labels/list): R8 action shape.
- [Gmail OAuth scopes](https://developers.google.com/workspace/gmail/api/auth/scopes): Least-scope connector choice.
- [Developer Certificate of Origin](https://developercertificate.org/): R11 sign-off semantics.

### Secondary: Medium Confidence

- [Biscuit Go usage](https://doc.biscuitsec.org/usage/go): Tagged v2 API behavior.
- [Biscuit specification](https://doc.biscuitsec.org/reference/specifications): Token and signature payload behavior.
- [AAT draft `-01`](https://www.ietf.org/archive/id/draft-niyikiza-oauth-attenuating-agent-tokens-01.html): Design context only.
- [OBO draft `-02`](https://www.ietf.org/archive/id/draft-oauth-ai-agents-on-behalf-of-user-02.html): Expired identity context only.
- First-party competitor documentation in [FEATURES.md](FEATURES.md): Positive feature evidence, checked 2026-08-11.

### Tertiary: Low Confidence

- Competitor feature absence: Use only `Not documented`, followed by a launch-day source refresh.
- Sole-author relicensing authority: Requires owner evidence or legal review outside repository research.

---
*Research completed: 2026-08-11*
*Ready for roadmap: yes*
