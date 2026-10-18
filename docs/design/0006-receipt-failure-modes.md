# ADR-0006: Explicit receipt failure modes

## Status

Proposed

## Date

2026-10-18

## Context

The [gateway](../../internal/gateway/gateway.go) returns 500 on receipt failure, withholding the outcome even after upstream execution.
[Ledger cancellation](../../internal/receipt/ledger.go) can leave committed receipts.
There is no fallback spool or failure-mode setting.

## Decision Drivers

- Keep durable evidence as the default admission requirement.
- Require a named failure class before adding fallback storage.
- Never interpret fail-open as bypassing authentication or policy.
- Make configuration changes auditable even when the primary sink fails.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| `fail_closed` only | Smallest failure surface | Local database faults stop all actions |
| Unbounded or memory-only fail-open | Highest apparent availability | Evidence loss and uncontrolled disk growth |
| Default closed, defer durable spool | Clear initial guarantee; avoids unsupported availability claims | No fallback until its failure model is approved |

## Decision

Propose shipping only `receipt_failure_mode: fail_closed` in the initial implementation.
These documents do not implement that behavior.
An unavailable durable intent sink prevents upstream dispatch and returns HTTP 503.
Failure after dispatch also returns 503 before response headers, without claiming the external action failed.
The unresolved committed intent is recovered as `outcome_unknown`.
This proposes an API behavior change from the current 500 response.

Defer `fail_open_spool` until the owner names a covered failure class and demonstrates recovery under that fault.
Reject that mode as unsupported for now.
A same-volume spool does not cover volume loss or full-disk failure.
It also cannot resolve an uncertain SQLite commit or replace a missing authorized signer.

### Authenticated Configuration

Log initial configuration and every later accepted mode change as signed v2 control receipts.
Bind old mode, new mode, actor, reason, and configuration digest.
The epoch key signs them with `control` authority.
Commit the control receipt before activating the configuration.
If it cannot commit, reject the change and retain the last committed configuration.
503 handling does not bypass authentication, action policy, or egress enforcement.

For human changes, require the existing [authenticated admin session](../../internal/admin/session.go).
Resolve `AdminID` and `OrgID` server-side and require installation configuration authority.
Apply [mutating-request CSRF checks](../../internal/admin/handler.go).
Never trust a request-supplied actor name.
The legacy shared `X-Admin-Secret` alone lacks an individual actor and is insufficient for this endpoint.
Bootstrap verifies the issuer-signed enrolment statement and binds its deployment identity to the configuration digest.
Unattended control authentication beyond that bootstrap remains deferred, not an anonymous local bypass.

## Consequences

### Positive

Default operation keeps a clear durability boundary.
No fallback availability is claimed without a demonstrated failure model.

### Negative

Primary-sink outages stop admission.
503 does not mean an already-dispatched external action was rolled back.

### Risks and Mitigations

Unlogged configuration can hide evidence loss. Require authenticated actors and committed control receipts before activation.
Started SSE cannot change HTTP status. Close the stream on terminal persistence failure and recover unknown completion later.

## Implementation Notes

1. Add 503 handling to the two-phase state machine.
   Acceptance: no intent acknowledgment means no upstream dispatch; post-dispatch failures never imply rollback.
2. Add authenticated configuration control.
   Acceptance: forged actors, wrong organizations, missing authority, and missing CSRF checks cannot change mode.
3. Fail the control append and request the deferred spool mode.
   Acceptance: neither change activates; the last committed mode survives restart.
4. Any later spool implementation must pass the appendix's crash and evidence-loss tests.

## Deferred Appendix: Bounded Durable Spool

This is a design sketch, not an enabled Phase 2 mode.
Approval requires a named primary-sink fault that leaves independent spool writes and signing safe.
Keep 1 writer and 1 authoritative chain.
Fsync intent before dispatch and outcome before response; preserve SSE's header and terminal boundaries.
Resolve uncertain commits before handoff. Never replay requests, re-sign receipts, or allocate replacement sequences.

Persist a signed sink-transition marker outside the spool before entering it.
Keep that marker in a separate durable control file, not solely SQLite or the spool directory.
Bind previous SQLite head, sink generation, spool identity, and fencing token.
Update its recorded spool head durably before acknowledging each spool append.
After a crash, scan durable spool records to reconcile any append newer than the marker.
An absent or corrupt spool while its marker remains is evidence loss, not permission to resume SQLite.
Refuse SQLite admission past a recorded spool head until exact signed records have been replayed and reconciled.
Only then fsync a return-to-SQLite marker and commit the corresponding control receipt.

Ship signed spool segments directly, without waiting for SQLite replay.
Their sealing metadata and upload progress must remain durable outside the failed SQLite sink.
Preserve the marker and pending segments if replay cannot finish.
Identical existing records are idempotent; conflicting positions halt recovery.
Delete nothing until replay, verified shipping, and retention requirements permit it.

Bound bytes, count, and age using owner-approved quotas.
Reserve intent, outcome, stream-terminal, and control capacity before admission.
Overflow rejects new work with 503 and never evicts evidence.
Tests must remove spool files while retaining the marker and interrupt every transition, replay, and upload boundary.
Acceptance: no SQLite restart can conceal a recorded spool suffix, and direct shipping works while SQLite remains unavailable.

## Related ADRs

- [0001: Chain positions](0001-per-replica-chains.md).
- [0002: Durability boundaries](0002-two-phase-receipts.md).
- [0003: Shipping and deletion](0003-durable-shipping.md).
- [0005: Signing authority](0005-root-certified-keys.md).
