# ADR-0007: Kubernetes identity, drain, and WAL ownership

## Status

Proposed

## Date

2026-10-18

## Context

This baseline includes [Compose](../../docker-compose.yaml) and [configuration YAML](../../configs/agentgate.yaml), not Helm or Kubernetes workload manifests.
The [health endpoint](../../internal/gateway/gateway.go) reports service count, not ledger readiness.
[HTTP shutdown](../../cmd/agentgw/main.go) currently allows 10 seconds.
Deferred closure then stops the ledger, clears signer keys, and closes SQLite.
[Ledger closure](../../internal/receipt/ledger.go) finishes an already submitted transaction but rejects pending requests.
It does not seal segments.

The same SQLite database also stores authorization, vault, and organization state.
Receipt isolation does not replicate that state.

## Decision Drivers

- Give every replica exclusive, restart-stable storage.
- Stop admission before draining work.
- Distinguish storage pressure from process failure.
- Preserve runtime-independent governance.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| StatefulSet with per-pod PVC | Stable attachment and explicit replica lifecycle | Volume fencing and zone recovery still need operations |
| Deployment with PVC per replica | Familiar rollout controller | Dynamic claim ownership needs extra identity machinery |
| Deployment with shared writable PVC | Simple manifest | Violates isolated writer and signer-cache assumptions |
| Ephemeral Deployment storage | Easy replacement | Loses committed, unshipped evidence |

## Decision

Propose a StatefulSet with 1 persistent data volume per replica.
Use `volumeClaimTemplates` and `ReadWriteOncePod` where supported.
Otherwise require storage-level single-writer fencing beyond `ReadWriteOnce`.
Test filesystem locking and fsync behavior.
Never share a writable SQLite database between replicas.

Persist the provisioned replica UUID and epoch alongside SQLite.
The StatefulSet ordinal locates its PVC but is not the cryptographic replica ID.
Replacement PVCs receive new UUIDs; intact restarts resume their epoch.
Retain PVCs on scale-down. Audit explicit deletion.

The only proposed gateway-owned writable state is its WAL data volume.
That includes the main SQLite file, WAL, SHM, encrypted keys, outbox, sealed files, and optional spool.
Existing authorization and vault rows remain on that volume.
Configuration and mounted secrets stay read-only.
Object storage and optional Tessera publication require separate durable infrastructure.
Consistent authorization and vault provisioning across replicas remains a rollout prerequisite.
Never copy live WAL files to provision replicas.

Propose separate `/livez` and `/readyz` endpoints plus a startup probe.
Liveness checks process progress, not upstream reachability or object-store availability.
Readiness requires completed recovery, exclusive volume ownership, an authorized signer, and admission capacity.
Spool mode requires healthy reserved capacity.
Upload outages preserve readiness until local quotas block admission.
Probes must not append synthetic receipts.

Propose an authenticated local drain control used by both `preStop` and SIGTERM.
First fail readiness and stop accepting new actions.
Allow endpoint propagation, then drain HTTP work.
Bound long-lived SSE streams and write their terminal or unknown records.
Wait for outstanding ledger append acknowledgments before calling `Ledger.Close`.
Seal the final segment and persist its outbox state.
Never wait indefinitely for upload.
Finally close the ledger, clear signer keys, and close SQLite.
Recover interrupted drain from that volume.

Propose `terminationGracePeriodSeconds: 60` as a starting budget.
Budget 5 seconds for endpoint propagation, 40 for requests and streams, and 10 for ledger completion and sealing.
Reserve 5 seconds for closure.
This requires changing the current shutdown timeout.
Hook time counts within the grace period.

For installations with at least 2 replicas, propose a PDB with `maxUnavailable: 1`.
Spread across hostnames; prefer zones when available.
Use rolling updates with at most 1 unavailable replica.
Document single-replica maintenance downtime.
PDBs neither prevent node failures nor replicate unshipped evidence.

Clawdlinux's operator applies sandbox labels and egress policy independently of the agent runtime.
Runtime selection belongs to the operator's `pkg/runtime.Registry`, not gateway branches.
Keep the gVisor label injector and configure runtime classes through that integration.
No runtime adapter should reimplement sandbox or egress policy.
Air-gapped profiles require no public-cloud endpoint.

## Consequences

### Positive

Replica identity follows durable ownership rather than pod scheduling.
Drain preserves local evidence without requiring remote availability.

### Negative

Stateful storage complicates rescheduling and disaster recovery.
Replica authorization provisioning remains separate work.

### Risks and Mitigations

A split-brain volume attachment can fork evidence. Require fencing and test forced node recovery.
Slow streams can exhaust shutdown time. Enforce drain deadlines and recover missing terminal records.
Topology spread can leave pods unschedulable. Use installation-specific constraints and document degraded availability.

## Implementation Notes

1. Add recovery-aware probes and idempotent drain control before publishing manifests.
   Acceptance: unhealthy storage removes readiness without triggering a restart loop.
2. Test a 2-replica rolling update with continuous traffic and SSE.
   Acceptance: no new attempts enter a draining pod; committed receipts survive rescheduling.
3. Kill a pod during drain and force a node replacement.
   Acceptance: outbox recovery resumes, and no two writers acquire the same replica identity.
4. Disconnect external networking and the optional upload destination.
   Acceptance: local governance and offline verification work; capacity exhaustion fails closed.
5. Validate equivalent sandbox labels and egress policy across supported operator runtimes.
   Acceptance: changing runtime selection does not change receipt or sealing guarantees.

## Related ADRs

- [0001: Identity](0001-per-replica-chains.md) governs volume loss and epochs.
- [0002: Lifecycle](0002-two-phase-receipts.md) supplies drain recovery records.
- [0003: Segments](0003-durable-shipping.md) defines sealing and retention.
- [0005: Rotation](0005-root-certified-keys.md) defines key expiry and drain interactions.
- [0006: Failure modes](0006-receipt-failure-modes.md) determines readiness capacity.
