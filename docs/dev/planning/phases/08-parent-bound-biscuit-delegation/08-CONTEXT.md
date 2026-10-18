---
phase: 08-parent-bound-biscuit-delegation
gathered: 2026-08-16
status: ready-for-planning
mode: autonomous (continuing established roadmap execution)
research: pkg.go.dev/github.com/biscuit-auth/biscuit-go/v2 and its README
  (fetched live during this phase), PRD-receipts-oss.md TASK-R7
---

<domain>
## Phase Boundary

DELG-01 through DELG-06: an agent can present an attenuated Biscuit token
alongside a `/v1/act` call; AgentGate verifies it (signature chain, then
binds agent/principal/service/action/expiry to the actual request) before
any registry, vault, or upstream access, and records the ordered
attenuation-path commitments in the receipt. Google Workspace (Phase 9)
and everything after it are untouched.
</domain>

<decisions>
## Repository moved; import path did not

`github.com/biscuit-auth/biscuit-go` renamed its GitHub organization to
`eclipse-biscuit/biscuit-go` (confirmed live: "Change the repository
organisation to eclipse-biscuit (#175)"). `go get
github.com/biscuit-auth/biscuit-go/v2@latest` still resolves to `v2.2.0`
through GitHub's repository-rename redirect. Using the old import path is
correct and matches what `go get` actually pins; no vendoring or fork was
needed.

## BlockCount and Code count attenuation blocks only, not the authority block

Empirically confirmed by writing and running a throwaway probe test
against the real library before committing to the "real" implementation:
`Biscuit.BlockCount()` is 0 for an authority-only (direct-grant) token, and
`Biscuit.Code()` returns one string per *attenuation* block — index 0 in
`Code()` is the first appended block, not the authority block. An initial
implementation assumed an off-by-one (authority counted at index 0) and
silently produced an empty `delegation_chain` for a 1-block-attenuated
token; the probe caught this before it shipped.

## One grant fact, not several separately-satisfiable facts

The authority block asserts exactly one 5-arity fact,
`grant(principal, agent, service, action, expiry)`, rather than five
separate single-value facts. A single ground tuple is much harder for an
attacker-controlled attenuation block to synthesize by recombining
unrelated facts than five independently-satisfiable ones would be.

## Two independent defenses against a widened or spliced grant (DELG-05), confirmed empirically

1. Biscuit's own signature chain: a token's `Authorizer(root)` call
   cryptographically verifies every block back to the root signature.
   Presenting one token's real, valid, *unmodified* bytes for a *different*
   request context (the practical, concrete form of "splicing a valid
   attenuation from chain A into chain B to widen scope" this phase can
   actually construct and test) is rejected because the request's bound
   facts (principal/agent/service/action) do not match what block 0 grants
   — confirmed by `TestVerify_SplicedTokenRejectedForWrongContext`.
2. A holder of a token can legitimately attenuate it further (append
   blocks) without the root's private key — that's the whole point of
   delegation. Verify closes the resulting gap explicitly: after Biscuit's
   own Datalog policy passes, it re-derives exactly which `grant(...)` fact
   satisfied the policy and calls `Biscuit.GetBlockID` on it, rejecting
   unless that fact lives in block 0. Empirically, biscuit-go v2.2.0's own
   default authorization-world scoping *already* rejects a holder-appended
   fake `grant(...)` fact at the `Authorize()` step (confirmed live via the
   probe and `TestVerify_WideningViaAppendedFakeGrantFactRejected`) — the
   `GetBlockID` check is kept anyway as an explicit, documented,
   independently-correct second line of defense that does not depend on
   this specific library version's default Datalog world-scoping behavior
   continuing to hold.

## A denied delegation is still receipted, not silently dropped

The delegation check originally sat inside `prepareAttempt` (the
receiptless pre-identity zone: unknown API keys and malformed request
bodies produce no receipt because there is no trustworthy identity yet to
attribute one to). It was moved to be the first step of `executeAttempt`
instead — by the time a delegation token is being checked, the API key and
request shape are both already valid, so a denial here is exactly the same
kind of event as an existing scope-denied case, and should produce the
same kind of evidence: a receipt with `policy_decision: "deny"`. An
un-receipted delegation failure would mean the one place a rejected
delegation attempt is most worth auditing produces no auditable record at
all.

## Fail closed when Delegation is unconfigured but a token is presented

`gateway.Config.Delegation` is optional (nil by default, matching the
existing `Limiter` pattern) so gateways that never call Phase 8 code paths
are unaffected. But if a request *presents* a delegation header on a
gateway with no configured verifier, that is treated as a denial (still
receipted), not as "ignore the header and proceed as an unmediated
request" — a caller's explicit claim of delegated authority must never be
silently dropped in a way that could look, to an auditor reading the
receipt later, like a direct, non-delegated grant.

## Root key: one persistent Ed25519 keypair, no rotation this phase

Modeled on `internal/signer.Store`'s AES-256-GCM at-rest encryption
(`internal/delegation.RootStore` derives its own purpose-specific key from
the same master secret via the already-exported `signer.DerivePurposeKey`,
never reusing signer's or vault's actual encryption key). Unlike signer
keys, this root has no `kid`, no validity interval, and no rotation
support — DELG-01 through DELG-06 do not ask for root rotation, and adding
it now would be speculative complexity with no requirement behind it.

## Issuance is a Go-level function, not an HTTP endpoint

TASK-R7 asks for verification and receipt-lineage binding, not an
admin-facing issuance API. `delegation.Issue` and `delegation.Attenuate`
are exported Go functions used directly by tests (and available to any
future in-process caller), deliberately not wired to any route. Adding an
HTTP issuance endpoint now would be scope beyond what any DELG-0X item
requires.
</decisions>

<code_context>
## Existing Code Insights

- `internal/receipt/receipt.go`'s `Receipt.DelegationChain []string` and
  `internal/receipt/ledger.go`'s `Draft.DelegationChain []string` already
  existed since Phase 2/4 (`// attenuation path, empty in R2`) — this phase
  is the first to ever populate them. `Validate` already caps the chain at
  32 elements of at most 64 ASCII bytes each; a hex-encoded SHA-256 digest
  is exactly 64 characters, fitting with no schema change.
- `internal/gateway/gateway.go`'s `attempt`/`outcome` split (from Phase 4)
  is reused directly: `prepareAttempt` now only *extracts* the raw
  delegation token (no verification, no receipt implications), and
  `executeAttempt` performs the actual `Verify` call as its very first
  step, before the pre-existing scope check.
- `internal/signer/store.go`'s AES-256-GCM encrypt/decrypt pattern and its
  exported `DerivePurposeKey` helper were reused as-is for
  `internal/delegation.RootStore`.
</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.
</specifics>

<deferred>
## Deferred Ideas

- Root key rotation: not required by any DELG-0X item.
- An HTTP issuance endpoint for Biscuit grants: not required; `Issue`/
  `Attenuate` remain Go-level only.
- Byte-level protobuf splicing (hand-corrupting a token's serialized wire
  format directly): the cryptographic signature-chain defense this would
  exercise is a property of biscuit-go itself, not of AgentGate's own code,
  and is considerably more brittle to maintain against future biscuit-go
  wire-format changes than the two application-level tests actually
  written.
</deferred>
