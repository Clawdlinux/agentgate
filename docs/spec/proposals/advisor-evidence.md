# Advisor evidence without receipt v1 changes

Status: implemented as a separate record. Receipt v1 stays frozen.
Phase 3 items 1 through 6 cover the advisor and evaluation scaffold.

## Fixed wire constraints

Receipt v1 accepts only `allow`, `deny`, and `rate_limited`.
Its `error` field accepts lowercase ASCII codes, not a fixed vocabulary.
We use the existing decision-record binding from [`DECISION.md`](../../../pkg/receiptspec/DECISION.md).
No receipt fields, preimage bytes, or version-lock vectors change.

| Advisor outcome | Receipt policy | Receipt error |
| --- | --- | --- |
| allow | Existing deterministic outcome | Existing error, usually empty |
| require_approval | `deny` | `advisor_escalated` |
| deny_by_advisor | `deny` | `advisor_escalated` |
| unavailable, allow_with_flag | Existing deterministic outcome | `advisor_unavailable`, unless another error already exists |
| unavailable, require_approval | `deny` | `advisor_unavailable` |
| unavailable, deny | `deny` | `advisor_unavailable` |

The record always carries the unavailable flag, including when another error exists.
`require_approval` returns HTTP 409. `deny_by_advisor` returns HTTP 403.
This phase does not implement an approval queue or automatic replay.
Approval never bypasses deterministic authorization on a later attempt.

## Binding

Without an advisor call, `params_sha256` retains its original params digest.
That includes disabled advisors, non-risky actions, and deterministic denials.

For an advisor call:

1. Compute the original gateway params digest without changing params canonicalization.
2. Compute SHA-256 over the input state's Go JSON encoding.
3. Build a versioned JSON evidence record.
4. Store the original params digest inside `original_params_sha256`.
5. Set receipt `params_sha256` to `receiptspec.HashRecord(record)`.
6. Store both objects in the ledger's existing SQLite transaction.
7. Commit before returning the action response.

The record contains:

- `record_version: 1` and `record_type: agentgate_advisor`.
- The original params digest and input state digest.
- The exact fixed questions.
- Approval, denial, and confidence thresholds.
- The service's returned cascade threshold, when available.
- The unavailable policy, flag, and bounded failure category.
- Client-observed call latency in microseconds.
- The advisor outcome and final receipt policy and error.
- Ordered tier attempts, including model IDs and pinned revisions.
- Per-tier weights SHA-256, probabilities, confidence, and latency.

Fractions are decimal strings. Latencies are integers.
This follows `CanonicalRecord`'s numeric restrictions.
Input params remain unchanged when sent upstream.
The record stores digests, not full request payloads.

An unavailable call can have no returned tier attempts.
We never fabricate model identity, weights, or probabilities after a timeout.
Returned valid attempts survive a service error.
Unreturned tier work cannot be reconstructed from the gateway.
This limitation needs a durable service journal before model deployments claim complete per-tier timeout evidence.

## Offline verification and export

The existing receipt JSONL export remains byte-compatible.
Fetch each companion record through admin-authenticated `GET /v1/receipts/{seq}/evidence`.
Alternatively, retain `receipt_records.record_json` from the SQLite backup.
The evidence endpoint checks binding and returns no record on failure.

First verify the receipt signature and chain with a trusted keyset.
Then call:

```go
err := receiptspec.VerifyRecordBinding(receipt, evidenceJSON)
```

To bind the original request too, compare its gateway params digest with `original_params_sha256`.
The state digest covers `{"action":...,"params":...,"service":...}` using Go's JSON encoding.
It does not authenticate any task claim inside the payload.

Deleting a record does not invalidate the receipt's signature.
It removes the evidence needed to explain that receipt's params digest.
Receipt v1 cannot independently enforce companion-record completeness.
Retain records with receipt exports and treat missing expected records as an audit failure.
Database append failures still return `receipt_failed`, never an uncommitted action response.
The existing SaaS-side-effect crash window remains unchanged.

## Proposed v2 shape

A future receipt version could preserve the direct params digest:

```json
{
  "format_version": 2,
  "params_sha256": "<original params digest>",
  "advisor_evidence_sha256": "<canonical evidence record digest>"
}
```

Its signed preimage must include the new digest and presence semantics.
An all-zero digest could mean no advisor call.
Verifiers must require the record whenever the signed digest is nonzero.
This proposal needs new vectors and explicit version negotiation.
It is not part of receipt v1.

## Phase 3 implementation plan and review notes

- Item 1: add the Go interface, bounded HTTP client, and escalation policy.
- Item 2: validate the registry's boolean `risky` marker.
- Item 3: check risky requests before vault access and upstream dispatch.
- Item 4: isolate Python inference behind the service API.
- Item 5: evaluate 60 synthetic cases, not nonexistent receipt logs.
- Item 6: keep default-off configuration and document runtime blockers.

The shipped risky action changes are:

| Action | Reason |
| --- | --- |
| GitHub `create_issue` | Writes repository state |
| Slack `post_message` | Sends outbound content |
| Slack `add_reaction` | Writes shared channel state |
| Stripe `create_charge` | Starts a payment |
| Stripe `create_payment_intent` | Starts a payment flow |

Markers appear in the shipped merged catalogs and corresponding connector catalogs.
No delete, outbound-email, or bulk-export actions currently ship in those catalogs.
Their evaluation cases are synthetic.

## Runtime blockers

The heuristic backend runs. It is not a trained model.
Both model adapters stop startup with `NotImplementedError`.
Published vllm-sr 0.4.0 source does not establish a Decision 2.0 state/questions inference contract.
We did not invent an API or add a custom weight loader.
See the [runtime inspection](../../../services/decision-advisor/RUNTIME.md).

No ONNX artifact was found at the inspected Kai or Laya revisions.
Kai's listed weight files exceed the 1.5 GB download cap.
Model evaluation and GPU cascade deployment remain pending.
The advisor stays off despite the synthetic harness results.
