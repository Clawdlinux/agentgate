# ADR-0006: Explicit receipt failure modes

## Status

Proposed

## Date

2026-10-18

## Context

The [gateway](../../internal/gateway/gateway.go) currently returns HTTP 500 when receipt append fails.
It never returns the would-be outcome without an acknowledged receipt commit.
An upstream side effect may already exist.
The [ledger](../../internal/receipt/ledger.go) notes that cancellation after submission can still leave a committed receipt.
There is no durable fallback spool or failure-mode setting today.

## Decision Drivers

- Keep durable evidence as the default admission requirement.
- Offer bounded availability during specific SQLite failures.
- Never interpret fail-open as bypassing authentication or policy.
- Make configuration changes auditable even when the primary sink fails.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| `fail_closed` only | Smallest failure surface | Local database faults stop all actions |
| Unbounded or memory-only fail-open | Highest apparent availability | Evidence loss and uncontrolled disk growth |
| Default closed, optional durable spool | Bounded fallback with explicit risk | More recovery states and capacity accounting |

## Decision

Propose `receipt_failure_mode: fail_closed` as the default.
An unavailable durable intent sink prevents upstream dispatch and returns HTTP 503.
Failure after dispatch also returns 503 before response headers, without claiming the external action failed.
The unresolved committed intent is recovered as `outcome_unknown`.
This proposes an API behavior change from the current 500 response.

Propose opt-in `fail_open_spool`, meaning open relative to the primary SQLite sink only.
It still requires durable signed evidence before dispatch and response.
Use a bounded append-only spool on the replica's persistent data volume.
Fsync its intent before upstream dispatch and its outcome before returning a buffered response.
For SSE, fsync the header outcome before headers and persist the terminal artifact afterward.
The spool is not a raw request queue. Never store credentials or replay upstream requests.

Keep 1 writer and 1 authoritative chain across SQLite and the spool.
Fallback is allowed only with an unambiguous committed head and an available certified signer.
An uncertain SQLite commit cannot be converted into a second signed entry.
Fence admission until durable reconciliation resolves that attempt.
Disk failure, signer failure, expired certificates, or unknown chain state still fail closed.
Sharing a volume means the spool does not protect against volume loss or full-disk failure.

Propose explicit limits for bytes, record count, and oldest unshipped age.
Initial suggested limits are 256 MiB, 100,000 records, and 24 hours.
Owner approval and workload sizing are required before enabling them.
Reserve maximum bounded intent, outcome, and stream-terminal space before admitting each attempt.
Reserve control-record capacity separately.
Never evict unshipped records.
When any limit blocks reservation, reject new attempts with 503 and `Retry-After`.
Inflight work consumes its reserved space, including recovery outcomes.

Log every configured mode change as a signed v2 control receipt.
Bind old mode, new mode, actor, reason, and configuration digest.
Also record effective sink transitions between SQLite and spool.
Commit that receipt to the current authoritative sink before activating the change.
If neither sink can durably record it, reject the change and retain the last committed configuration.
Bootstrap records the initial configuration before admission.
Do not silently downgrade modes on restart.

On recovery, reconcile spool and SQLite positions before accepting traffic.
Replay exact signed records into SQLite with their original identities and bytes.
Identical existing entries are idempotent; different hashes at the same position halt recovery.
Do not allocate replacement sequences, re-sign, or re-dispatch upstream.
Ship spooled evidence through the same sealed-segment protocol.
Delete spool files only after replay, shipping verification, and retention requirements are satisfied.

## Consequences

### Positive

Default operation keeps a clear durability boundary.
Opt-in fallback survives some primary-sink faults without inventing successful attestations.

### Negative

Capacity reservation and sink handoff add complexity.
503 does not mean an already-dispatched external action was rolled back.

### Risks and Mitigations

Ambiguous handoff can fork a chain. Require fencing and durable reconciliation before selecting another sink.
Spool exhaustion can strand outcomes. Reserve terminal capacity before dispatch and test worst-case record sizes.
Started SSE cannot change HTTP status. Close the stream on terminal persistence failure and recover unknown completion later.

## Implementation Notes

1. Implement the two-phase state machine before exposing fallback configuration.
   Acceptance: neither mode dispatches without a durable intent.
2. Inject unambiguous database failures and uncertain commit acknowledgments separately.
   Acceptance: only the former can enter the spool; the latter blocks until reconciled.
3. Fill each quota and exhaust reserved control capacity.
   Acceptance: no evidence is evicted, and unrecordable mode changes are refused.
4. Restart during sink handoff, spool fsync, replay, and segment shipping.
   Acceptance: exact replay produces no duplicates, forks, or upstream calls.

## Related ADRs

- [0001: Chains](0001-per-replica-chains.md) defines unique positions.
- [0002: Two-phase receipts](0002-two-phase-receipts.md) defines durability boundaries.
- [0003: Shipping](0003-durable-shipping.md) governs deletion and acknowledgment.
- [0005: Keys](0005-root-certified-keys.md) defines signing availability.
