---
phase: 08-parent-bound-biscuit-delegation
plan: 08-01
completed: 2026-08-16
---

# Phase 8 Plan 01: Parent-Bound Biscuit Delegation — Summary

## What Was Built

- **`internal/delegation`** (new package): binds attenuated Biscuit tokens
  (`github.com/biscuit-auth/biscuit-go/v2`) to verified AgentGate requests
  and receipt lineage.
  - `doc.go`: package doc citing `draft-niyikiza-oauth-attenuating-agent-
    tokens` and `draft-oauth-ai-agents-on-behalf-of-user-02` as design
    context, explicitly disclaiming any wire-format compliance claim
    (DELG-06).
  - `rootstore.go`: `RootStore`, a single persistent Ed25519 keypair
    (AgentGate's Biscuit trust root), AES-256-GCM encrypted at rest using
    the same pattern as `internal/signer.Store`, deriving its own
    purpose-specific key via `signer.DerivePurposeKey`. No rotation, no
    `kid` — this root is a single fixed trust anchor for this milestone.
  - `issue.go`: `Issue` (builds and signs a single-block "direct grant"
    biscuit encoding `grant(principal, agent, service, action, expiry)`)
    and `Attenuate` (appends one restricting block to an existing token —
    the delegation path a holder can extend without the root's private
    key).
  - `verifier.go`: `Verifier.Verify` — checks the token's full signature
    chain against the trusted root, then requires the request's actual
    principal/agent/service/action to match the token's grant fact exactly
    and the grant to not be expired, and returns the ordered, privacy-safe
    attenuation-path commitments (one SHA-256 digest per attenuation
    block, hex-encoded, never the raw token or block source text).
- **`internal/db/migrations/003_delegation_root.sql`**: the
  `delegation_root_key` table (single row, id=1).
- **`internal/gateway/gateway.go`**: wired in as an optional dependency
  (`Config.Delegation`, nil-safe like the existing `Limiter` pattern).
  `prepareAttempt` now only extracts the raw `X-Agentgate-Delegation`
  header (base64); `executeAttempt` performs the actual verification as
  its first step, before the pre-existing scope check — satisfying
  DELG-01's "before registry, vault, or upstream access" ordering — and
  populates `receipt.Draft.DelegationChain` from the result.
- **`cmd/agentgw/main.go`**: wires `delegation.NewRootStore` +
  `LoadOrCreateRoot` + `delegation.NewVerifier` into the gateway's
  `Config.Delegation`, alongside the existing signer/vault/auth wiring.

## Key Design Decisions Realized

- **A single 5-arity `grant(...)` fact**, not five separate facts — makes
  it structurally harder for an attacker-controlled attenuation block to
  synthesize a false match by recombining unrelated facts.
- **Two independent defenses against widening/splicing**: Biscuit's own
  signature-chain verification (rejects reusing one token's real bytes for
  a different request context) plus an explicit `Biscuit.GetBlockID`
  check requiring the matched grant fact to originate from block 0 (kept
  as defense-in-depth even though biscuit-go v2.2.0's own default
  Datalog-world scoping already rejects the tested widening attempt at the
  `Authorize()` step).
- **A denied delegation is still receipted** (`policy_decision: "deny"`),
  moved out of the receiptless pre-identity zone (`prepareAttempt`) into
  the receipted zone (`executeAttempt`), because by the time a delegation
  token is checked the caller already has a verified API key and a
  schema-valid request — this is exactly the same class of event as an
  existing scope-denied case, and produces the same kind of auditable
  evidence.
- **Fail closed on an unconfigured verifier**: a request presenting a
  delegation header against a gateway with `Config.Delegation == nil` is
  denied (and receipted), never silently treated as an unmediated direct
  request.

## Bugs Found During Implementation

- **`BlockCount`/`Code` off-by-one.** An initial implementation assumed
  `Biscuit.BlockCount()` included the authority block (so a 1-attenuation
  token would report count 2, and `Code()[0]` would be the authority).
  Both assumptions were wrong: `BlockCount()` counts only attenuation
  blocks (0 for a direct grant), and `Code()` has one entry per
  *attenuation* block only. This was caught by writing and running a
  throwaway probe test against the real library (`go test -run TestProbe`)
  before writing the "real" implementation and test suite — the probe's
  debug output showed `BlockCount= 1 Code()= [Block {...}]` for a
  single-attenuation token, immediately surfacing the mismatch. Fixed in
  `attenuationChain` before any real test was written against the wrong
  assumption.
- **No other implementation bugs found.** The widening-defense-in-depth
  behavior (`GetBlockID`) was verified to be currently redundant with
  biscuit-go's own default Datalog scoping via the same probe, which is
  valuable information in itself: it confirms the belt-and-suspenders
  check is not covering for a bug, and documents exactly what would need
  to change in biscuit-go's behavior for that check to start actually
  matter.

## Test Coverage Added

- `internal/delegation/verifier_test.go`: direct grant passes with a nil
  chain (DELG-04); every one of principal/agent/service/action mismatching
  alone is denied (DELG-02, table-driven); expired grants denied; a token
  signed by an untrusted root denied; an attenuated grant returns an
  ordered, hex-only, non-identical-per-block chain (DELG-03, DELG-04); a
  fully valid token reused for a different context is denied (DELG-05); a
  holder-appended fake wider `grant(...)` fact is denied while the token's
  real original scope still verifies (DELG-05); a malformed token is
  denied.
- `internal/delegation/rootstore_test.go`: create-then-reload returns the
  identical persisted keypair; a *second, independent* `RootStore`
  instance over the same database loads the same root rather than
  generating a new one; a non-32-byte master key is rejected.
- `internal/gateway/delegation_test.go` (full HTTP-level, real upstream
  test server with a call counter): no delegation header leaves behavior
  and the receipted chain exactly as before Phase 8; a verified direct
  grant reaches the upstream and records an empty chain; a
  mismatched-context token is denied with zero upstream calls and one
  `policy_decision: "deny"` receipt recording `error: "delegation_denied"`;
  a delegation header presented against an unconfigured verifier fails
  closed, also receipted as a deny.

## Residual Risks / Follow-ups

- No HTTP issuance endpoint exists for Biscuit grants — not required by
  any DELG-0X item; `delegation.Issue`/`Attenuate` are Go-level only.
- The root key never rotates within this milestone.
- The `GetBlockID`-based widening defense is not currently exercised as
  the *actual* blocking check in any test (biscuit-go's own Authorize()
  already blocks it first) — this is documented explicitly in
  `08-CONTEXT.md` rather than hidden, since it is relevant to anyone later
  reviewing why that code path exists.

## Verification Commands Run

```
go build ./...
go vet ./...
gofmt -l .                          # clean, no output
go test ./... -race -count=1        # all packages pass, including the
                                     # new internal/delegation package
```
