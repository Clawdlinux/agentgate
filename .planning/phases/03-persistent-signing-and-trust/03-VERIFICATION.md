---
phase: 03-persistent-signing-and-trust
verified: 2026-08-14T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 3: Persistent Signing and Trust Verification Report

**Phase Goal:** Operators retain a durable Ed25519 signer whose public history supports independent verification.
**Baseline:** `ddcc7f3`
**Verified:** 2026-08-14
**Status:** `passed`
**Re-verification:** No. This is the initial verification.

## Verdict

The signer package satisfies KEY-01 through KEY-07. All 5 roadmap success criteria have direct, passing test coverage. All required executable gates pass.

## Goal Achievement

### Observable Truths

| # | Roadmap truth | Status | Evidence |
|---|---|---|---|
| 1 | First persistent startup creates an Ed25519 keypair and stores its private material encrypted with a purpose-derived key | VERIFIED | `TestLoadOrCreateActive_GeneratesOnFirstStartup`; `DerivePurposeKey` + AES-256-GCM in `store.go` |
| 2 | Restart preserves signer identity, while unreadable persistent key material stops startup without silent replacement | VERIFIED | `TestLoadOrCreateActive_RestartPreservesIdentity`; `TestLoadOrCreateActive_UnreadableKeyFailsClosed` |
| 3 | `GET /v1/receipts/pubkey` returns active and historical public keys with deterministic IDs and no private material | VERIFIED | `TestPubkeyHandler_ExposesKeysWithoutPrivateMaterial`; `TestComputeKID_DeterministicAndBoundToPublicKey` |
| 4 | An operator can rotate keys while old receipts remain verifiable within explicit sequence intervals | VERIFIED | `TestRotate_PreservesOldKeyForVerification`; `TestRotate_RejectsNonIncreasingSequence` |
| 5 | An auditor can validate a receipt using only a separately trusted public root and public key history | VERIFIED | `TestVerify_RequiresOnlyPublicKey`; `Verify`/`ComputeKID` take only public bytes, no store dependency |

**Score:** 5/5 roadmap truths verified.

## Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| KEY-01 | SATISFIED | `LoadOrCreateActive` generates via `ed25519.GenerateKey(rand.Reader)` on first row absence |
| KEY-02 | SATISFIED | Private key encrypted with AES-256-GCM under `DerivePurposeKey(masterKey, "agentgate.signer.v1")` |
| KEY-03 | SATISFIED | Identity persists across `Store` re-instantiation with the same key; wrong key returns `ErrKeyUnreadable` with zero rows changed |
| KEY-04 | SATISFIED | `PubkeyHandler` serves `PublicKeys()`; `KeyRecord` has no private-key field |
| KEY-05 | SATISFIED | `ComputeKID` = `"ed25519:" + hex(sha256(pub))[:16]`, deterministic and collision-free in test |
| KEY-06 | SATISFIED | `Rotate` closes old interval at `atSeq-1`, opens new interval at `atSeq`; old key still verifies pre-rotation signatures |
| KEY-07 | SATISFIED | `Verify`/`ComputeKID` are pure functions over `ed25519.PublicKey` only |

No Phase 3 requirement is orphaned. REQUIREMENTS.md maps KEY-01 through KEY-07 to Phase 3 and R3.

## Scope Verification

Diff contains exactly 9 new files: `internal/signer/{signer,keyderive,store,handler}.go` and their `_test.go` counterparts, `internal/db/migrations/002_signer_keys.sql`, `internal/db/sqlite_test.go`, and `.planning/phases/03-persistent-signing-and-trust/03-CONTEXT.md`.

No file changed under `cmd/agentgw`. Production composition (mounting the handler, replacing the in-memory vault) remains Phase 4 scope per PROJECT.md's explicit assignment (LEDG-02).

## Behavioral Checks

| Check | Result |
|---|---|
| `go test ./internal/signer/... -race -count=1` | PASS, 9/9 |
| `go test ./internal/db/... -count=1` | PASS, 1/1 (migration applies cleanly and idempotently) |
| `go test ./... -count=1` | PASS, all packages |
| `go vet ./...` | PASS |
| `gofmt -l internal/signer internal/db` | PASS, no output |
| `git diff --check` | PASS |

## Anti-Patterns

No `TBD`, `FIXME`, `XXX`, `TODO`, `HACK`, placeholder, or empty implementation appears in the new files.

## Residual Risks

1. `gofmt -l .` (repo-wide) flags 3 pre-existing files (`internal/gateway/gateway.go`, `internal/gateway/gateway_test.go`, `internal/registry/registry.go`) unrelated to this phase's diff. Not introduced by Phase 3; not fixed here to keep scope bounded.
2. The partial unique index on `active = 1` is SQLite's cross-process backstop; the primary correctness guard is the in-process `sync.Mutex`, consistent with this milestone's single-instance scope.
3. `AGENTGATE_VAULT_KEY` vs `VAULT_ENCRYPTION_KEY` env var naming mismatch (pre-existing, noted in Phase 3 CONTEXT.md) remains for Phase 4 to resolve during production wiring.

## Gaps Summary

None. All 5 success criteria verified, all 7 requirements satisfied, all executable gates pass.

---

_Verified: 2026-08-14_
_Verifier: GitHub Copilot, autonomous phase-verification pass_
