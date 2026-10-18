# ADR-0002: Durable intent and outcome receipts

## Status

Proposed

## Date

2026-10-18

## Context

The [gateway](../../internal/gateway/gateway.go) executes upstream work before appending its v1 receipt.
It returns the response only after that append commits.
The [ledger](../../internal/receipt/ledger.go) documents the resulting external-side-effect crash gap.
An upstream action can succeed without a later committed receipt.

[SSE](../../internal/gateway/stream.go) starts only after a header-time receipt commits.
Final stream digests currently reach logs, not signed receipts.
The [stream-digest proposal](../spec/proposals/receipt-stream-digest.md) is unimplemented.

## Decision Drivers

- Record a durable obligation before dispatch.
- Report uncertainty without replaying external side effects.
- Preserve the receipt-before-response boundary, including SSE headers.
- Measure the extra durable write rather than claiming free batching.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Outcome only | Current latency and simple schema | Crash can erase evidence of an action |
| Intent only | Records attempted dispatch | Cannot attest observed outcomes |
| Durable intent, then outcome | Exposes missing outcomes | Adds latency and recovery state |

## Decision

Propose v2 lifecycle records, not new meanings for v1 fields.
After authentication, shape validation, and policy evaluation, allocate a gateway-generated attempt ID.
Persist a signed `intent` before any upstream call, including credential refresh.
The intent binds identity, delegation, params digest, policy, and destination action.
Denied attempts get a terminal decision record without an upstream intent.

An acknowledged intent permits 1 logical execution under the existing safe retry policy.
An append timeout can still leave a committed record.
Resolve that attempt's durable state before dispatch; ambiguity must not trigger an upstream retry.
Provider retries and idempotency remain separate policies. Receipts do not guarantee exactly-once external execution.

Append a signed `outcome` after observing the upstream result.
It references the intent's `chain_id`, sequence, entry hash, and attempt ID.
For buffered responses, commit the outcome before returning status or body.
Persist outcomes with a bounded context detached from client cancellation, matching the current gateway boundary.

For SSE, commit an `outcome` with `observation = headers_accepted` before forwarding headers.
That outcome does not claim stream completion.
Append a supplemental `stream_result` at termination.
It binds the outcome hash, written-byte SHA-256, byte count, and termination reason.
The digest covers bytes accepted by downstream writes, not proven client delivery.
A missing terminal record means unknown completion, never success.
Keep terminal artifacts out of forwarded SSE bytes.
This proposed lifecycle replaces the earlier stream proposal's single-action-receipt rule for v2 only.

Before admitting traffic after restart, scan committed intents without outcomes.
Append recovery outcomes marked `outcome_unknown`. Never call upstream during recovery.
For header outcomes without terminal stream records, append `stream_result` with unknown termination and no fabricated digest.
Fenced recovery and unique references must prevent duplicate terminal records.
Recovery records can reference an earlier epoch.

### Group-Commit Cost

Intent and outcome cannot share a transaction for their own request.
Other requests can still share each batch's fsync.
Allowed buffered attempts require 2 durable boundaries instead of 1.
SSE adds a later terminal record as well.

`bench/RESULTS.md` is absent on the pinned main baseline.
[Phase 0 PR #50](https://github.com/Clawdlinux/agentgate/pull/50), head `29ef207`, reports these added-overhead p99 measurements:

| Workload | Existing 1-receipt path |
| --- | --- |
| Closed loop, 0 ms upstream, concurrency 1 | 0.34 ms |
| Closed loop, 0 ms upstream, concurrency 16 | 3.42 ms |
| Closed loop, 0 ms upstream, concurrency 64 | 6.22 ms |
| Open loop, 0 ms upstream, 25% load | 7.00 ms |

The PR used Apple M4, `GOMAXPROCS=4`, and 20,000 requests per row.
It did not meet the under-1-ms p99 gate.
Those are end-to-end overheads, not isolated fsync measurements.
Doubling them would not predict two-phase p99.
Budget 1 extra serialized commit wait before dispatch, then benchmark it on Linux with dedicated 4-vCPU capacity.

## Consequences

### Positive

Crashes leave inspectable obligations.
Offline verification can distinguish observed results from unknown outcomes.

### Negative

Write volume and request latency increase.
Recovery requires lifecycle indexes beyond a linear hash chain.

### Risks and Mitigations

An unknown outcome may hide an executed action. Require operator reconciliation, not automatic replay.
SSE can fail after response headers. End the stream and persist uncertainty when storage recovers.

## Implementation Notes

1. Freeze lifecycle references and optional fields in [v2](../spec/RECEIPT-v2.md).
   Acceptance: reject missing, duplicate, or mismatched intent references.
2. Add intent ordering instrumentation.
   Acceptance: the mock upstream sees no call before intent commit acknowledgment.
3. Run subprocess `kill -9` tests before intent commit, after commit, during dispatch, and after provider success.
   Also kill between the upstream call and outcome commit, after outcome commit, and during SSE.
   Acceptance: recovery is idempotent, records unknown states, and never re-dispatches.
4. Re-run Phase 0 closed-loop and open-loop workloads.
   Acceptance: publish intent-wait and outcome-wait percentiles separately; obtain owner approval for the latency budget.

## Related ADRs

- [0001: Chains](0001-per-replica-chains.md) supplies stable references.
- [0003: Shipping](0003-durable-shipping.md) retains recovery obligations.
- [0006: Failure modes](0006-receipt-failure-modes.md) defines alternate durable recording.
- [0007: Drain](0007-kubernetes-lifecycle.md) bounds unfinished streams.
