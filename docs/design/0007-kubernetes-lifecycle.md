# ADR-0007: Kubernetes identity, drain, and WAL ownership

## Status

Proposed

## Date

2026-10-18

## Context

This baseline has [Compose](../../docker-compose.yaml) and [configuration YAML](../../configs/agentgate.yaml), not Kubernetes workload manifests.
The [health endpoint](../../internal/gateway/gateway.go) reports service count, not ledger readiness.
[HTTP shutdown](../../cmd/agentgw/main.go) currently allows 10 seconds.
Then [ledger closure](../../internal/receipt/ledger.go) finishes submitted transactions but rejects pending requests, without sealing.
Signer and database closure follow.

SQLite also stores authorization, vault, and organization state. Receipt isolation does not replicate it.

## Decision Drivers

- Preserve identity and exclusive storage.
- Drain safely without restart loops.
- Keep governance runtime-independent.

## Considered Options

| Option | Pros | Cons |
| --- | --- | --- |
| StatefulSet with per-pod PVC | Stable attachment and identity | Storage fencing and zone recovery |
| Deployment with PVC per replica | Familiar controller | Additional claim-ownership machinery |
| Shared or ephemeral storage | Simple scheduling | Writer conflicts or evidence loss |

## Decision

Propose a StatefulSet with 1 persistent data volume per replica.
Use `volumeClaimTemplates` and `ReadWriteOncePod` where supported.
Otherwise require storage fencing beyond `ReadWriteOnce`. Test filesystem locking and fsync.
Never share a writable SQLite database between replicas.

Persist the provisioned replica UUID and epoch alongside SQLite.
Ordinals locate PVCs; they are not cryptographic identities.
Empty PVCs stay unready until explicit intermediate enrolment.
Never silently initialize replacement identity.
The signed registry binds ordinal, new UUID, and replacement of the prior UUID.
Report the old chain lost from its last shipped or checkpointed head, with unknown suffix.
Without coverage, report it lost with no known head.
Intact restarts require recovery and lease checks before resuming.
Retain PVCs on scale-down. Audit explicit deletion.

Check [0001's cached lease view and token](0001-per-replica-chains.md) before admission and each batch, without per-batch API calls.
Background renewal sets a monotonic self-fencing deadline, approximately 10 seconds with the 5-second/15-second defaults.
Keep that deadline shorter than lease duration minus maximum clock drift; self-fence when it expires.
Kubernetes Lease expiry alone does not fence a partitioned node.
Force-detach requires proven old-node storage fencing, then lease takeover with a newer token.
Loss declarations and same-UUID takeovers require a `trust_status` cutoff at the last covered head before activation.
Verifiers reject the affected epoch's suffix beyond that head; a takeover must use a newly certified epoch.
Unproven fencing blocks takeover.

The only proposed gateway-owned writable state is its WAL data volume.
It includes SQLite, WAL, SHM, encrypted keys, outbox, sealed files, and any future spool.
Existing authorization and vault rows remain on that volume.
Configuration and mounted secrets stay read-only.
Object storage and optional Tessera publication require separate durable infrastructure.
Multi-replica AgentGate cannot ship until per-replica auth/vault state is solved.
Test provisioning, rotation, and revocation propagation before enabling replicas beyond 1.
Never copy live WAL files to provision replicas.

Propose separate `/livez` and `/readyz` endpoints plus a startup probe.
Liveness checks process progress, not upstream or object storage.
Readiness checks recovery, exclusive volume ownership, signer authority, and capacity.
The initial deployment is fail-closed. The deferred spool is neither required nor enabled.
Only later spool-enabled deployments require [0006's spool protocol](0006-receipt-failure-modes.md).
Upload outages preserve readiness until local quotas block admission.
Probes must not append synthetic receipts.

Use authenticated local drain control for `preStop` and SIGTERM.
Fail readiness and stop admission.
Allow endpoint propagation, then drain HTTP work.
Bound long-lived SSE streams and write their terminal or unknown records.
Wait for append acknowledgments before `Ledger.Close`.
Seal the final segment and persist its outbox state.
Never wait indefinitely for upload.
Close ledger, clear signer keys after descriptor durability, then close SQLite.
Recover interrupted drain from that volume.

Propose `terminationGracePeriodSeconds: 60`, including hooks.
Allocate 5 seconds for propagation, 40 for requests/streams, 10 for ledger/sealing, and 5 for closure.
Change the current shutdown timeout accordingly.
The 40-second stream budget cuts longer SSE streams.
The expected `stream_result` has `termination = unknown` and reason `drain_deadline`.
Any digest covers only the observed prefix.
Document these rollout cuts; unlimited stream continuity is not promised.

For 2 or more replicas, propose a PDB with `maxUnavailable: 1`.
Spread across hostnames; prefer zones when available.
Use rolling updates with at most 1 unavailable replica.
Document single-replica maintenance downtime.
PDBs neither prevent node failures nor replicate unshipped evidence.

Clawdlinux's operator applies sandbox labels and egress policy independently of runtime.
Select runtimes through its `pkg/runtime.Registry`, not gateway branches.
Keep its gVisor label injector and runtime-class integration.
Adapters must not reimplement sandbox or egress policy.
Air-gapped profiles require no public-cloud endpoint.

## Consequences

### Positive

Replica identity follows durable ownership rather than pod scheduling.
Drain preserves local evidence without requiring remote availability.

### Negative

Stateful storage complicates rescheduling and disaster recovery.
Unsolved replica authorization state blocks scale-out release.

### Risks and Mitigations

Split-brain attachments can fork evidence. Require lease and storage fencing.
PVC loss loses unshipped evidence. Preserve registry history and report loss.
Slow streams exhaust shutdown time. Cut them and record unknown termination.
Spread constraints can block scheduling. Document installation-specific degraded availability.

## Implementation Notes

1. Add recovery-aware probes and idempotent drain.
   Acceptance: unhealthy storage removes readiness without restart loops.
2. Test a 2-replica rolling update with continuous traffic and SSE.
   Acceptance: draining pods reject new attempts; committed receipts survive rescheduling.
3. Partition a node with active upstream work, expire its lease, then request CSI force-detach.
   Attempt takeover before and after proving storage fencing.
   Acceptance: unfenced takeover fails; replacement tokens increase; the old writer cannot admit or publish.
   Acceptance: renewal failure self-fences on monotonic deadline; takeover status rejects records beyond the last covered head.
4. Replace its PVC with an empty one.
   Acceptance: enrolment gates readiness; registry records replacement and old-chain loss.
5. Drain an SSE stream longer than 40 seconds.
   Acceptance: stream cuts produce unknown termination, never completion.
6. Disconnect external networking and the optional upload destination.
   Acceptance: local governance and offline verification work; capacity exhaustion fails closed.
7. Validate equivalent sandbox labels and egress policy across supported operator runtimes.
   Acceptance: changing runtime selection does not change receipt or sealing guarantees.

## Related ADRs

- [0001: Fencing and volume loss](0001-per-replica-chains.md).
- [0002: Drain recovery](0002-two-phase-receipts.md).
- [0003: Sealing](0003-durable-shipping.md).
- [0005: Enrolment and rotation](0005-root-certified-keys.md).
- [0006: Dependency only for enabled spool](0006-receipt-failure-modes.md).
