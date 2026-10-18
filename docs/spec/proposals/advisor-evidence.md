# Advisor companion receipts

Status: implemented without changing receipt v1 fields or gateway params hashing.
This replaces the initial Phase 3 binding design.

## Frozen meaning

Every gateway action receipt keeps `params_sha256 = DigestParams(params)`.
Advisor checks, truncation, and unavailable handling never change that digest.
The advisor is a separate decision producer under [`DECISION.md`](../../../pkg/receiptspec/DECISION.md).
Only its companion receipt uses `HashRecord(record)`.
The companion also uses the reserved `AgentKeyID="agentgate:advisor"`.
Service, action, and this producer ID must all match before verification treats a receipt as an advisor companion.
Modern API key IDs are 32 lowercase hex characters.
Authentication rejects the reserved ID for legacy rows and cached identities too.
Normal agent IDs cannot create this producer identity.
Older receipts with matching names and ordinary agent IDs remain ordinary receipts.

`prepareAttempt` rejects service `advisor` and actions beginning `decision:` with HTTP 400 and no receipt.
The service reservation is case-insensitive and also applies during registry loading.
These checks apply while the advisor is disabled too.

| Advisor outcome | Action policy | Companion policy | Error code |
| --- | --- | --- | --- |
| allow | Existing deterministic outcome | `allow` | Existing error, usually empty |
| require_approval | `deny` | `deny` | `advisor_escalated` |
| deny_by_advisor | `deny` | `deny` | `advisor_escalated` |
| unavailable, allow_with_flag | Existing deterministic outcome | `allow` | `advisor_unavailable`, unless another error exists |
| unavailable, require_approval | `deny` | `deny` | `advisor_unavailable` |
| unavailable, deny | `deny` | `deny` | `advisor_unavailable` |
| input rejected or truncated | `deny` | `deny` | `advisor_input_rejected` |

Input rejection requires approval even when the unavailable policy is `allow_with_flag`.
A configured unavailable policy of `deny` stays stricter for rejected inputs.
Valid high-risk answers can still deny truncated requests.
Approval returns HTTP 409. Advisor denial returns HTTP 403.
There is no approval queue or automatic replay.

## Atomic pair

The ledger performs these steps within one existing `commitBatch` transaction:

1. Seal and insert the action receipt, preserving its params digest.
2. Add `action_receipt` to the canonical evidence record.
3. Include that receipt's sequence, entry hash, and params digest in the link.
4. Seal a companion with `Service="advisor"` and `Action="decision:advise"`.
5. Set its `params_sha256` to `HashRecord(record)`.
6. Set its `prev_hash` to the action receipt's entry hash.
7. Insert the companion immediately after the action receipt.
8. Insert the record into `receipt_records`, keyed by the companion sequence.
9. Commit both receipts and the record before acknowledging the action.

No other request can interleave receipts within this pair.
The record uses a decimal string for the action sequence.
This preserves the full receipt sequence range under decision-record numeric restrictions.
The action and companion share the signing-key lease.
Only the companion's agent identity becomes the reserved producer ID.
An insertion failure rolls back the entire pair.

Record link:

```json
{
  "action_receipt": {
    "seq": "41",
    "entry_hash": "<action receipt entry hash>",
    "params_sha256": "<original gateway params digest>"
  }
}
```

The record also retains model identities, pinned revisions, and weights SHA-256.
It includes questions, probabilities, thresholds, latency, and unavailable handling.
Fractions use decimal strings.
Ordered tier attempts preserve returned cascade evidence.
Unavailable calls can have no returned tier attempts.
We never fabricate unknown model identities or probabilities.
Unreturned tier work still needs a durable service journal before complete timeout evidence can be claimed.

## Input budget and task signals

Advisor JSON encoding disables HTML escaping.
Complete params have a 256 KiB budget.
Larger params become a bounded UTF-8 JSON prefix inside `params._truncated_json`.
The upstream action params and their receipt digest remain untouched.
The record sets `truncated: true` and `input_rejected: true`.
It binds both the transmitted state digest and original state digest.
Truncation requires approval even if a safe answer returns.

Local serialization or size failures and service 4xx responses are `advisor_input_rejected`.
Redirects and invalid success responses also require approval.
Only timeouts, connection failures, and service 5xx responses qualify for `allow_with_flag`.
The record distinguishes rejected input from unavailable inference.

The state shape is exactly `{"service":...,"action":...,"params":...}`.
There is no separate task header or trusted task signal in this phase.
The heuristic ignores `params.stated_task` and top-level task claims.
They cannot raise confidence or establish task consistency.
Ordinary params are still forwarded upstream unchanged.
The heuristic reports unknown task consistency and low confidence.

## Export and verification

Receipt JSONL lines, receipt preimages, and the manifest format stay unchanged.
Default exports retain the frozen v1 format and omit record lines.
Released v0.1.x verifiers can still verify their receipt chains.
Request `GET /v1/receipts/export?from=1&records=1` to opt into record lines.
This extension adds a typed record line immediately after each companion receipt:

```json
{"type":"record","seq":42,"record":{"outcome":"require_approval","action_receipt":{"seq":"41","entry_hash":"...","params_sha256":"..."}}}
```

The manifest counts receipts, not record lines.
The extension is outside the frozen default JSONL format.
Updated readers accept both existing bundles and the additive record lines.
Updated advisor evidence verification needs this opt-in export or the SQLite source.
Older readers reject the extension. Do not send it to them.
No evidence is hidden inside frozen receipt fields.

The offline JSONL and SQLite verifier checks:

1. Existing receipt signatures and chain linkage.
2. `VerifyRecordBinding(companion, record)`.
3. The record's action sequence, entry hash, and params digest against the preceding receipt.
4. Companion adjacency and its coarse outcome mapping.

Missing, duplicate, orphaned, or tampered records fail advisor evidence verification.
A record-enabled range can start with a companion.
Its manifest must pass signature verification first.
The companion's `prev_hash` must equal the signed anchor hash.
The record's action sequence and entry hash must match the signed anchor sequence and hash.
No preceding action receipt is needed for those comparisons.
The anchor does not reveal the original params digest.
Include the preceding action receipt to compare that digest independently.
Manifest-less bundles beginning with a companion still fail.
Admin-authenticated `GET /v1/receipts/{companion_seq}/evidence` remains available.

Receipt v1 cannot enforce that every risky action has a companion.
A range can end with an action receipt before its companion.
The signed chain alone does not encode the registry marker or enabled advisor configuration.
Completeness of advisor coverage needs external configuration and checkpoint evidence.
The existing SaaS-side-effect crash window is unchanged.

## Proposed v2 shape

A future signed receipt could add `advisor_evidence_sha256` while retaining the original params digest.
An all-zero digest would mean no advisor check.
A nonzero digest would require a matching record.
This needs new version negotiation and vectors.
It is not implemented in v1.

## Risky action review notes

| Shipped action | Reason |
| --- | --- |
| GitHub `create_issue` | Writes repository state |
| Slack `post_message` | Sends outbound content |
| Slack `add_reaction` | Writes shared channel state |
| Stripe `create_charge` | Starts a payment |
| Stripe `create_payment_intent` | Starts a payment flow |

The markers remain in merged and connector catalogs.
No delete, outbound-email, or bulk-export action currently ships in those catalogs.
Their harness cases are synthetic.

## Remaining runtime blockers

The heuristic runs. It is not a trained model.
Both model adapters still fail startup with `NotImplementedError`.
Published vllm-sr 0.4.0 source does not establish a verified Decision 2.0 inference contract.
No custom weight loader or guessed API was added.
See [RUNTIME.md](../../../services/decision-advisor/RUNTIME.md).
The advisor remains off by default.
