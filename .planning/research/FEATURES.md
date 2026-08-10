# Feature Landscape

**Domain:** Agent action receipts and OSS launch
**Project:** AgentGate Receipts and OSS Launch
**Researched:** 2026-08-11
**Overall confidence:** HIGH for project scope. MEDIUM for market comparisons.

## Classification Summary

| Capability | Category | Priority | Task | Complexity | Launch position |
|------------|----------|----------|------|------------|-----------------|
| Apache 2.0 license and NOTICE | Table stake | P0 | R1 | Low, with legal review | Blocks all other work |
| Deterministic private receipt | Differentiator | P0 | R2 | High | Blocks signing and verification |
| Ed25519 signing and key history | Differentiator | P0 | R3 | High | Blocks independent verification |
| Gap-free synchronous receipt chain | Differentiator | P0 | R4 | High | Blocks verifier and launch |
| Offline SQLite and JSONL verifier | Differentiator | P0 | R5 | High | Blocks launch |
| Five-minute GitHub quickstart | Table stake | P0 | R9 | Medium | Final launch gate |
| Bounded JSONL export | Table stake | P1 | R6 | Medium | First follow-up |
| Parent-bound Biscuit delegation | Differentiator | P1 | R7 | High | Follows export |
| Google Workspace connector | Table stake | P1 | R8 | Medium | Changes the featured set |
| Sourced competitor comparison | Table stake | P1 | R10 | Medium | Requires shipped facts |
| Contributor guide and starter issues | Table stake | P1 | R11 | Low | Last follow-up |
| External receipt checkpoints | Deferred differentiator | P2 | Later | High | Not in this milestone |
| PostgreSQL receipt storage | Deferred scale feature | P2 | Later | High | Not in this milestone |
| DPDP field mapping | Deferred sales feature | P2 | Later | Medium | Requires a stable receipt format |

## Fixed Delivery Order

The PRD defines this release path:

```text
R1 -> R2 -> R3 -> R4 -> R5 -> R9 -> OSS launch
OSS launch -> R6 -> R7 -> R8 -> R10 -> R11
```

Each task gets one pull request. R2 through R5 form one product block, not one pull request.

No task may bypass an earlier acceptance gate. P1 work must not delay the OSS launch.

Target all P1 work within 2 weeks after launch.

## Table Stakes

These features make the repository usable and credible. They are not the main product claim.

### P0 Launch Blockers

#### R1: Apache 2.0 Release Basis

**Why expected:** Outside contributors need clear rights before they inspect contribution tasks.

**Dependencies:** Sole-author relicense authority must be confirmed first.

**Acceptance implications:**

- Add the exact Apache License 2.0 text as `LICENSE`.
- Add `NOTICE` as required by the PRD.
- Replace all 4 BSL source headers.
- Update the README license section.
- `rg "Business Source" --glob '*.go' .` returns no matches.
- Complete R1 before receipt code or contributor outreach.

**Scope fence:** Do not add a CLA, foundation process, or dual-license program.

#### R9: Five-Minute GitHub Quickstart

**Why expected:** A receipt product is not usable until a new operator can produce and verify one.

**Dependencies:** R1 through R5 must pass first. R9 verifies SQLite, so it does not require R6.

**Acceptance implications:**

- Put the quickstart before architecture details in the README.
- Start from a clean checkout with Git and Docker installed.
- Do not require a local Go toolchain.
- Include every GitHub OAuth setup step in the timed run.
- Run the shipped Compose command without editing source files.
- Connect one GitHub account through a documented route.
- Send one authenticated `POST /v1/act` request.
- Run the shipped verifier against SQLite with networking disabled.
- Show a successful chain and exit code `0`.
- Record the timing method and finish below 5 minutes.
- Repeat the run with someone new to the repository.

The claim cannot exclude OAuth client setup without saying so. If the full path exceeds 5 minutes, do not publish the claim.

The current Compose path needs a launch test. The image references one combined service file, while the repository stores separate YAML files.

**Scope fence:** Do not add a web UI, hosted demo, Kubernetes path, or local Go requirement.

### P1 Follow-Ups

#### R6: Bounded JSONL Export

**Why expected:** An auditor needs a portable artifact instead of direct database access.

**Dependencies:** R5 defines the accepted JSONL representation and key input.

**Acceptance implications:**

- Expose `GET /v1/receipts/export?from=&to=`.
- Reuse the existing admin authentication boundary.
- Define `from` and `to` as inclusive sequence numbers.
- Reject invalid, reversed, or excessive ranges.
- Return sequence-ordered `application/x-ndjson`.
- Keep raw parameters, tokens, and upstream bodies out of the export.
- Verify a full export with the same offline CLI.
- Treat a range beginning at sequence 1 as a full-chain verification.
- Treat a later starting sequence as a partial-range verification.
- Verify the first partial receipt's signature, then every internal link.
- Print `VALID PARTIAL RANGE`, not `VALID CHAIN`, for a partial range.
- Test first, middle, last, empty, and out-of-range selections.
- Test modified, missing, and inserted records inside a partial range.

A partial range proves signatures and internal links. It does not prove global completeness without an external anchor.

**Scope fence:** Do not add PDF reports, cloud storage, dashboards, or public unauthenticated export.

#### R8: Google Workspace Featured Connector

**Why expected:** A launch connector needs a common, low-risk demonstration action.

**Dependencies:** Follow R7 in delivery order. Reuse the existing registry and OAuth paths.

**Acceptance implications:**

- Keep GitHub and Slack featured.
- Replace Stripe only in featured documentation.
- Keep the Stripe config and SDK support working.
- Keep the featured set at exactly 3 connectors.
- Implement one Gmail-backed Google Workspace config.
- Start with `users.labels.list` and the narrow `gmail.labels` scope.
- Add registry tests for method, path, parameters, and scope.
- Test OAuth linking and one live read action separately from unit tests.

The registry supports one base URL per service. Do not bundle Gmail, Drive, and Calendar into one connector.

**Scope fence:** Do not start a connector catalog or add restricted Gmail scopes for the demo.

#### R10: Sourced Comparison

**Why expected:** Security claims need evidence. A comparison page without sources weakens trust.

**Dependencies:** R8 fixes the featured connector story. Receipt and verifier claims must already pass tests.

**Acceptance implications:**

- Create `docs/comparison.md`.
- Keep the required rows: Nango, Composio, Astrix, Oasis, and AgentGate.
- Keep the 5 required capability columns from the PRD.
- Link each positive competitor claim to first-party public documentation.
- Mark missing public evidence as `Not documented`.
- Do not convert missing evidence into a definitive `No`.
- Add a checked date for every competitor source set.
- Recheck sources immediately before merge and before launch publication.
- Source AgentGate claims from shipped docs and passing tests.
- Remove any claim that cannot be reproduced or sourced.

**Scope fence:** This is a feature-presence table. It is not a benchmark or vendor scorecard.

#### R11: Contribution Path

**Why expected:** Contributors need a visible path from checkout to a focused pull request.

**Dependencies:** R1 establishes contribution terms. Publish guidance before opening starter issues.

**Acceptance implications:**

- Add root-level `CONTRIBUTING.md`.
- Document prerequisites, build, test, lint, and focused pull request steps.
- Explain `git commit -s` and the `Signed-off-by` line.
- State that milestone tasks use one pull request each.
- Open exactly 6 independently testable `good first issue` tasks.
- Use 4 service-config tasks and 2 verifier-output tasks.
- Give every issue files, acceptance checks, and a no-secrets test path.
- Apply the `good first issue` label only after the guide lands.

**Scope fence:** Do not promise maintainership, paid bounties, or response times.

## Differentiators

These features carry the receipt claim. Missing any P0 item makes the launch claim false.

### R2: Deterministic, Privacy-Preserving Receipt

**Value:** An auditor can compare and verify stable evidence without receiving request bodies.

**Dependencies:** R1 only.

**Acceptance implications:**

- Freeze the exact field order and length-prefixed encoding.
- Choose a format version before creating the golden fixture.
- Define the genesis sequence and zero predecessor hash.
- Hash a canonical decoded parameter map with SHA-256.
- Do not hash raw JSON whitespace or map insertion order.
- Persist only `params_sha256`, never raw parameters.
- Keep `human_principal` as an opaque local identifier.
- Store a bounded error code, not an upstream response body.
- Keep `delegation_chain` empty until R7.
- Test empty strings, empty parameters, Unicode input, and maximum integer values.
- Make 2 independent encoders produce identical bytes.
- Pin one fixed golden fixture in the repository.

**Scope fence:** Do not adopt a new receipt protocol or a generic event schema.

### R3: Ed25519 Signing With Verifier-Safe Keys

**Value:** An auditor can verify signatures without gaining forging authority.

**Dependencies:** R2 fixes the signed bytes.

**Acceptance implications:**

- Use Go's standard `crypto/ed25519` package.
- Generate the first keypair from a secure random source.
- Encrypt the private key with the existing vault key.
- Persist key material across gateway restarts.
- Assign a stable, unique `signer_kid`.
- Return current and retained public keys from `/v1/receipts/pubkey`.
- Include algorithm and encoding details for each key.
- Never return the private key or encrypted private-key blob.
- Verify old receipts after restart and rotation.
- Reject duplicate key IDs and unknown signing algorithms.

A bundled public key proves internal consistency only. Auditor identity requires a separately pinned key or fingerprint.

R3 and R5 must define that trust input. Offline verification cannot fetch it during the run.

**Scope fence:** Do not port HMAC. Do not require signer state or network access for verification.

### R4: Gap-Free Synchronous Receipt Chain

**Value:** The request-path gateway records an ordered history that exposes internal tampering.

**Dependencies:** R3 signing, SQLite migration `002_receipts.sql`, and the owning action handler.

**Recommended emission boundary:** Every authenticated, schema-valid action attempt gets exactly one receipt.

This includes policy denial, missing or expired tokens, rate limits, upstream errors, and upstream responses.

Malformed and unauthenticated traffic stays in security logs. Those requests lack the required human and agent bindings.

**Acceptance implications:**

- Use one completion path for all admitted action outcomes.
- Allocate sequence, predecessor hash, signature, and insert atomically.
- Fail the action if the synchronous receipt write fails.
- Never acknowledge an action whose receipt was dropped.
- Preserve `audit_log` and existing consumers.
- Continue sequence and predecessor state after restart.
- Verify 100 concurrent action calls with no gaps or duplicates.
- Test success, deny, rate limit, token failure, and upstream failure.
- Measure added p99 latency during R4.
- Revisit the write design only if added p99 exceeds 50 ms.

**Scope fence:** Do not add an asynchronous queue, buffer overflow path, or PostgreSQL backend.

### R5: Independent Offline Verifier

**Value:** A third party can detect tampering without the gateway, private key, or network.

**Dependencies:** R4 produces the authoritative chain. R3 defines trusted public-key input.

**Acceptance implications:**

- Support SQLite, JSONL files, and JSONL from stdin.
- Require a separately saved trusted key set through `--keyset`.
- Stream records in sequence order.
- Recompute canonical bytes and every entry hash.
- Check sequence continuity, predecessor linkage, key ID, and Ed25519 signature.
- Support every retained historical signer key.
- Exit `0` only for a valid chain.
- Exit `1` for chain, hash, or signature mismatch.
- Exit `2` for I/O, parsing, missing keys, or invalid configuration.
- Treat an empty source as invalid input with exit code `2`.
- Report the first failing sequence without printing sensitive fields.
- Pass with all network interfaces unavailable.
- Test modified, middle-deleted, inserted, and forged records separately.
- Test malformed JSONL, unknown key IDs, empty logs, and rotated keys.

A local chain cannot detect unanchored tail truncation. Do not claim detection until P2 checkpointing ships.

**Scope fence:** The verifier reports failures. It does not repair, rewrite, upload, or fetch keys.

### R7: Parent-Bound Attenuated Delegation

**Value:** Receipt evidence records how delegated authority narrowed before an action ran.

**Dependencies:** R6 in delivery order. R4 provides receipt emission and R5 verifies the resulting receipt chain.

**Acceptance implications:**

- Use the verified `/v2` Biscuit Go module path after a fresh dependency check.
- Verify each Biscuit before action lookup or upstream execution.
- Require every child to remain equal to or narrower than its parent.
- Bind the child to its exact parent, principal, service, action, and argument limits.
- Reject expired, over-depth, widened, and wrongly rooted chains.
- Reject a valid child block spliced into another parent chain.
- Store stable block or token commitments in `delegation_chain`.
- Never store raw Biscuit tokens in receipts.
- Leave the chain empty for direct grants.
- Test rejection before any upstream request is sent.

The AAT draft uses JWT and JWS. Biscuit uses public-key tokens and Datalog checks.

AgentGate may follow draft semantics. It must not claim wire compatibility or IETF compliance.

The OBO draft `-02` is expired. Treat it as identity and consent context only.

**Scope fence:** Do not invent a delegation token format or authorization server.

## P2 Deferrals

| Capability | Why deferred | Dependency before reconsidering | Acceptance implication later |
|------------|--------------|---------------------------------|------------------------------|
| Rekor or Sigstore checkpoint mirroring | External anchoring is not needed for developer adoption | Stable P0 receipt and verifier behavior | Define checkpoint trust and tail-truncation tests |
| PostgreSQL receipt storage | SQLite meets the single-tenant milestone | Measured SQLite limits or deployment demand | Preserve canonical bytes and verifier behavior |
| DPDP consent-artifact mapping | It is an enterprise documentation wedge | Shipped and stable receipt fields | Map only fields that exist in released artifacts |

P2 work must not enter R1 through R11 pull requests.

## Anti-Features

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| HMAC receipt signatures | A verifier holding the secret can forge receipts | Use Ed25519 public verification |
| Raw request parameters | Exports would expose action data | Persist a canonical SHA-256 digest |
| Raw errors or response bodies | Provider data may contain secrets or personal data | Store a bounded error code |
| Buffered or lossy receipt writes | Missing entries invalidate completeness claims | Write synchronously and fail closed |
| Verifier network calls | Offline review becomes dependent on gateway state | Use a pinned local public-key set |
| Embedded key as sole trust root | An attacker can replace the log and key together | Pin the signer key or fingerprint separately |
| Silent receipt repair | Repair hides evidence of tampering | Report the first failure and stop |
| Claiming all deletions are detected | Tail truncation needs an external anchor | Limit P0 claims to observable gaps and tampering |
| Replacing `audit_log` | Existing consumers may break | Add the receipt table through migration 002 |
| New delegation token specification | It adds protocol risk and review scope | Use Biscuit and track IETF draft semantics |
| IETF compliance marketing | Both cited documents are Internet-Drafts | Name exact revisions and describe alignment |
| More than 3 featured connectors | Breadth is not the product wedge | Feature GitHub, Slack, and Google Workspace |
| Multi-API Google connector | The registry has one base URL per service | Start with one Gmail action set |
| Connector-specific receipt formats | Verification would fragment by provider | Keep one provider-neutral receipt schema |
| Dashboard or hosted verifier | It adds a second product surface | Keep CLI and HTTP interfaces |
| Kubernetes, Helm, or gVisor packaging | It delays the Docker adoption path | Keep Docker Compose |
| Multi-tenant isolation | The milestone remains single tenant | Revisit only with a separate requirement |
| PostgreSQL in R1 through R11 | It adds migration and operating work | Keep SQLite for this milestone |
| Rekor or Sigstore in R1 through R11 | It expands the trust model before launch | Keep it in P2 |
| DPDP mapping before format stability | Documentation would chase schema changes | Map the released receipt later |
| ANF performance claims | Those measurements belong to another product | Measure only R4 write overhead |
| Unsourced competitor negatives | Missing documentation is not proof of absence | Say `Not documented` and link searched sources |
| Unsupported benchmark numbers | They are not needed for feature comparison | Compare documented feature presence only |
| Starter issues before contributor docs | Contributors lack setup and acceptance guidance | Land `CONTRIBUTING.md` first |

## Feature Dependencies

```text
Apache license
  -> canonical receipt
  -> Ed25519 signing and key history
  -> synchronous SQLite chain
  -> offline verifier
  -> timed GitHub quickstart
  -> OSS launch

Offline verifier
  -> bounded JSONL export
  -> attenuated delegation evidence
  -> final featured connector set
  -> sourced comparison
  -> contributor starter issues
```

Additional dependency rules:

- R2 owns canonical bytes and privacy behavior.
- R3 owns signer identity, rotation, and historical public keys.
- R4 owns exact receipt timing and action-outcome coverage.
- R5 owns trust inputs, parsing, exit codes, and tamper diagnostics.
- R6 cannot define a second JSONL representation.
- R7 cannot change the P0 receipt envelope without a version decision.
- R8 cannot add a second connector framework.
- R10 cannot claim a feature before its acceptance tests pass.
- R11 cannot open starter issues before contributor instructions land.

## Comparison Claim Requirements

| Competitor claim | Required source | Current first-party evidence | Allowed wording |
|------------------|-----------------|------------------------------|-----------------|
| Nango issues connected access | Auth or connections documentation | Connections API lists provider connections without credentials | `Documented` with link |
| Nango sits in execution path | Action or proxy documentation | Actions execute provider calls with the human's credentials | `Documented` with link |
| Composio issues connected access | Connected-account documentation | Execute API accepts a connected account and human identifier | `Documented` with link |
| Composio sits in execution path | Tool execution API | The API executes a named tool against the provider | `Documented` with link |
| Astrix inventories access | Product documentation | Product page documents continuous NHI and agent inventory | `Documented` with link |
| Oasis inventories access | Product documentation | Product material documents inventory, ownership, and usage evidence | `Documented` with link |
| Any competitor emits signed receipts | Receipt, security, or audit documentation | No explicit evidence in sources reviewed on 2026-08-11 | `Not documented` only |
| Any competitor supports offline third-party verification | Verifier or export documentation | No explicit evidence in sources reviewed on 2026-08-11 | `Not documented` only |

R10 must repeat this search. Competitor capabilities can change before that pull request lands.

## MVP Recommendation

Ship only P0 before the OSS announcement:

1. R1 establishes legal contribution terms.
2. R2 through R4 create signed, private, gap-free evidence.
3. R5 makes the evidence independently testable.
4. R9 proves a new operator can reach the verified result.

Do not substitute a receipt demo for verifier completion. Do not move export or connector work ahead of R9.

After launch, deliver P1 in the fixed order. Keep all 3 P2 items outside this milestone.

## Sources

### Project Sources

- [Project definition](../PROJECT.md)
- [Receipts and OSS PRD](../../PRD-receipts-oss.md)
- [Current README](../../README.md)
- [Gateway request path](../../internal/gateway/gateway.go)
- [Current lossy audit logger](../../internal/audit/logger.go)
- [Service registry](../../internal/registry/registry.go)
- [Docker Compose file](../../docker-compose.yaml)
- [Docker image](../../Dockerfile)

### Cryptography and Delegation

- [Go `crypto/ed25519`](https://pkg.go.dev/crypto/ed25519). HIGH confidence.
- [Biscuit introduction](https://doc.biscuitsec.org/getting-started/introduction). HIGH confidence for Biscuit behavior.
- [`biscuit-go/v2` package](https://pkg.go.dev/github.com/biscuit-auth/biscuit-go/v2). HIGH confidence for the verified module path.
- [Attenuating Authorization Tokens `-01`](https://www.ietf.org/archive/id/draft-niyikiza-oauth-attenuating-agent-tokens-01.html). MEDIUM confidence because it is an Internet-Draft.
- [On-Behalf-Of User Authorization `-02`](https://www.ietf.org/archive/id/draft-oauth-ai-agents-on-behalf-of-user-02.html). LOW confidence for future direction because it expired.

### Competitor Sources

- [Nango action functions](https://docs.nango.dev/docs/getting-started/use-cases/actions). HIGH confidence for positive claims.
- [Nango observability](https://docs.nango.dev/docs/guides/platform/observability). HIGH confidence for log and proxy trace claims.
- [Nango connections API](https://docs.nango.dev/docs/reference/backend/http-api/connections/list). HIGH confidence for connection claims.
- [Composio tool execution](https://docs.composio.dev/reference/api-reference/tools/postToolsExecuteByToolSlug). HIGH confidence for request-path claims.
- [Composio execution logs](https://docs.composio.dev/docs/observability/logs). HIGH confidence for log claims.
- [Astrix NHI discovery](https://astrix.security/product/discover-non-human-identities/). MEDIUM confidence for product claims.
- [Oasis NHI management](https://www.oasis.security/blog/non-human-identity-management). MEDIUM confidence for product claims.

Absence claims remain LOW confidence. Public documentation may omit private or newly released features.

### Connector and OSS Sources

- [Gmail label listing](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.labels/list). HIGH confidence.
- [Gmail OAuth scopes](https://developers.google.com/workspace/gmail/api/auth/scopes). HIGH confidence.
- [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). HIGH confidence.
- [GitHub contribution guidelines](https://docs.github.com/en/communities/setting-up-your-project-for-healthy-contributions/setting-guidelines-for-repository-contributors). HIGH confidence.
- [Developer Certificate of Origin 1.1](https://developercertificate.org/). HIGH confidence.
