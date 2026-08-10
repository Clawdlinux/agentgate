# AgentGate Receipts and OSS Launch

## What This Is

AgentGate is a Go gateway that lets AI agents call SaaS APIs for humans without exposing OAuth tokens. This milestone adds independently verifiable receipts and prepares the repository for outside contributors.

## Core Value

Every agent action produces evidence that an independent auditor can verify offline without holding AgentGate's secret key.

## Requirements

### Validated

- [x] Agents can invoke registered SaaS actions through `POST /v1/act`.
- [x] An AES-256-GCM encrypted SQLite vault implementation exists and is covered by tests.
- [x] Database-backed API key storage and scope-checking implementations exist.
- [x] GitHub, Slack, and Stripe service configurations exist.
- [x] OAuth handlers, rate limiting, a Go SDK, and integration tests exist as reusable packages.
- [x] Audit events can be stored in SQLite, but they are not tamper evident.

### Active

- [ ] Publish the repository under Apache License 2.0 before other milestone work.
- [ ] Emit deterministic receipts that hash request parameters instead of storing raw values.
- [ ] Chain each receipt to its predecessor without sequence gaps.
- [ ] Sign receipts with Ed25519 and publish verifier-safe public keys by key ID.
- [ ] Verify SQLite and JSONL receipt logs offline with deterministic exit codes.
- [ ] Detect modified, deleted, inserted, and forged receipt records.
- [ ] Export bounded receipt ranges as verifier-compatible JSONL.
- [ ] Bind attenuated Biscuit delegation chains to their parent grants.
- [ ] Add Google Workspace to the featured launch connectors while retaining Stripe support.
- [ ] Deliver a clean-machine quickstart from clone to verified receipt in under five minutes.
- [ ] Publish sourced competitor comparisons and contribution guidance.
- [ ] Seed six scoped `good first issue` tasks after contributor documentation lands.

### Out of Scope

- A new delegation token specification. AgentGate implements toward existing IETF drafts.
- A web UI or dashboard. The milestone exposes CLI and HTTP interfaces only.
- More than three featured launch connectors. Connector breadth is not the differentiator.
- Kubernetes, Helm, or gVisor packaging. Docker Compose remains the adoption path.
- Multi-tenant isolation. The current product remains single tenant.
- Receipt performance marketing. ANF token-reduction results do not apply to AgentGate.
- Rekor or Sigstore checkpoint mirroring. This remains a later milestone.
- PostgreSQL receipt storage. SQLite is the current milestone backend.
- DPDP consent-artifact field mapping. This follows a shipped receipt format.

## Context

The gateway already contains about 4,000 lines of Go and a passing test suite. Existing packages cover authentication, OAuth, proxying, routing, rate limiting, encrypted token storage, and the SDK.

The current audit logger performs plain SQLite inserts through a buffered channel. Its overflow branch drops entries, which cannot support a gap-free receipt chain.

The production command does not yet compose all existing persistence and policy packages. It uses an in-memory vault and a plaintext API-key map, so request-path receipt work must wire verified identities and persistent state before making authorization claims.

The canonical hash-chain reference is `../agentic-operator-core/pkg/audit/chain.go`. Its length-prefixed encoding is reusable, but its HMAC signature is not.

The offline verification reference is `../agentic-operator-core/cmd/audit-verify/main.go`. AgentGate must replace shared-secret verification with Ed25519 public-key verification.

`PRD-receipts-oss.md` defines the milestone order, acceptance cases, launch boundary, and product claims. Current source and older architecture docs disagree about some runtime wiring. Phase research must identify the owning request path before implementation.

## Constraints

- **Compatibility**: Extend the existing gateway. Do not rewrite working packages.
- **License**: Apache License 2.0 must land before receipt code or contributor outreach.
- **Cryptography**: Use Ed25519. HMAC cannot support independent third-party verification.
- **Privacy**: Persist only a SHA-256 digest of request parameters. Never store raw parameters in receipts.
- **Consistency**: Receipt writes are synchronous and gap free. Correctness takes priority over throughput.
- **Storage**: Keep `audit_log` intact and add receipts through migration `002_receipts.sql`.
- **Verification**: Offline verification cannot require network access or signer state.
- **Performance**: Measure synchronous write overhead in the request-path phase. Revisit if p99 overhead exceeds 50 ms.
- **Delivery**: Preserve the PRD task order. Use one pull request per task with signed-off commits.
- **Adoption**: The clean-machine path uses Docker Compose and completes in under five minutes.
- **Claims**: Competitor comparisons link to first-party public sources and contain no unsupported benchmarks.
- **Legal**: Confirm sole-author relicense authority before merging the Apache licensing phase.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Keep the existing hash-chain construction | The core implementation already defines a documented canonical encoding | Pending |
| Replace HMAC-SHA256 with Ed25519 | Auditors must verify without gaining forging capability | Pending |
| Hash request parameters | Receipts must be shareable without leaking request bodies | Pending |
| Write receipts synchronously | Buffered writes can drop entries and invalidate the chain | Pending |
| Preserve the legacy audit table | Existing consumers must not break during migration | Pending |
| Resolve key rotation during signing work | Verification must account for every historical signer key ID | Pending |
| Feature GitHub, Slack, and Google Workspace | These services reduce demo friction while Stripe remains available | Pending |
| Launch only after offline verification and quickstart land | A receipt without verifier adoption is not the product claim | Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition:**
1. Move invalidated requirements to Out of Scope with a reason.
2. Move verified requirements to Validated with the phase reference.
3. Add newly discovered requirements to Active.
4. Record consequential implementation decisions.
5. Update the product description if scope changes.

**After each milestone:**
1. Review every section against shipped behavior.
2. Confirm the core value still controls prioritization.
3. Recheck every Out of Scope reason.
4. Update context with adoption evidence and operating results.

---
*Last updated: 2026-08-11 after initialization*