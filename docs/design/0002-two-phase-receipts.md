# ADR-0002: Durable intent and outcome receipts

## Status

Proposed

## Date

2026-10-18

## Context

The [gateway](../../internal/gateway/gateway.go) executes upstream work, commits its v1 receipt, then responds.
The [ledger](../../internal/receipt/ledger.go) documents the crash gap: an action can succeed before its receipt commits.

[SSE](../../internal/gateway/stream.go) starts only after a header-time receipt commits.
Digests reach logs, not receipts. The [stream-digest proposal](../spec/proposals/receipt-stream-digest.md) is unimplemented.

## Decision Drivers

- Record a durable obligation before dispatch.
- Report uncertainty without replaying external side effects.
- Preserve receipt-before-response, including SSE headers.
- Measure added commit latency.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| Outcome only | Current latency and simple schema | Crash can erase evidence of an action |
| Intent only | Records attempted dispatch | Cannot attest observed outcomes |
| Durable intent, then outcome | Exposes missing outcomes | Adds latency and recovery state |

## Decision

Propose v2 lifecycle records without changing v1.
After authentication, validation, and policy evaluation, allocate an attempt ID.
Persist a signed `intent` before any upstream call, including credential refresh.
Bind identity, delegation, params digest, policy, action, and any validated client request ID.
Denied attempts get a terminal decision record without an upstream intent.

An acknowledged intent permits 1 logical execution under existing safe retries.
Append timeout can still leave a committed record. Resolve ambiguity before dispatch, never by retrying upstream.

Append a signed `outcome` after observing the upstream result.
It references the intent's `chain_id`, sequence, entry hash, and attempt ID.
Include `upstream_attempt_count`, counting actual action dispatches, including transport retries.
Exclude credential refresh calls.
Use `observation = not_dispatched` only when the running process knows no action dispatch occurred.
That observation requires a known zero count, not an invented HTTP status.
Crash recovery still writes `outcome_unknown`, with unknown attempt count unless durable evidence establishes it.
For buffered responses, commit the outcome before returning status or body.
Use a bounded commit context detached from client cancellation, as today.

For SSE, commit an `outcome` with `observation = headers_accepted` before forwarding headers.
That outcome does not claim stream completion.
Append a supplemental `stream_result` at termination.
Bind outcome hash, written-byte SHA-256, byte count, and termination reason.
The digest covers downstream writes, not proven client delivery.
A missing terminal record means unknown completion, never success.
Keep terminal artifacts out of forwarded SSE bytes.
This proposed lifecycle replaces the earlier stream proposal's single-action-receipt rule for v2 only.

Before admitting traffic after restart, scan committed intents without outcomes.
Append recovery outcomes marked `outcome_unknown`. Never call upstream during recovery.
For header outcomes without terminal stream records, append `stream_result` with unknown termination and no fabricated digest.
Fenced recovery and unique references must prevent duplicate terminal records.
Recovery records can reference an earlier epoch.
Rotation does not drain inflight requests; their outcomes and stream results can enter the next epoch.

### Client Retry Linkage

Validate bounded `client_request_id` syntax; bind it to the principal and destination in the intent.
Defer client retry deduplication and mapping an attempt ID to upstream `Idempotency-Key`.
Providers differ in key support and retention. A new gateway attempt can repeat an external side effect.
Claim no exactly-once behavior; add no unsupported provider header.
The [owner defaults](README.md#open-decisions) disable automatic replay.

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
These are end-to-end overheads, not fsync measurements. Doubling them cannot predict two-phase p99.
Budget 1 extra serialized commit wait before dispatch, then benchmark it on Linux with dedicated 4-vCPU capacity.

## Consequences

### Positive

Crashes leave inspectable obligations and explicit unknown outcomes.

### Negative

Writes and latency increase. Recovery needs lifecycle indexes.

### Risks and Mitigations

An unknown outcome may hide an executed action. Require operator reconciliation, not automatic replay.
SSE can fail after response headers. End the stream and persist uncertainty when storage recovers.

## Implementation Notes

1. Freeze lifecycle references and optional fields in [v2](../spec/RECEIPT-v2.md).
   Acceptance: reject missing, duplicate, or mismatched intent references.
2. Add intent ordering instrumentation.
   Acceptance: the mock upstream sees no call before intent commit acknowledgment.
   Acceptance: runtime cancellation before dispatch yields `not_dispatched` and zero attempts; retry counts include every action dispatch.
3. Run subprocess `kill -9` tests before intent commit, after commit, during dispatch, and after provider success.
   Also kill between the upstream call and outcome commit, after outcome commit, and during SSE.
   Acceptance: recovery is idempotent, records unknown states, and never re-dispatches.
4. Re-run Phase 0 closed-loop and open-loop workloads.
   Acceptance: publish intent-wait and outcome-wait percentiles separately; obtain owner approval for the latency budget.

## Related ADRs

- [0001: References](0001-per-replica-chains.md).
- [0003: Shipping](0003-durable-shipping.md).
- [0006: Closed failure mode](0006-receipt-failure-modes.md).
- [0007: Stream drain](0007-kubernetes-lifecycle.md).
