---
phase: 03-persistent-signing-and-trust
plan: 01
completed: 2026-08-14
requirements: [KEY-01, KEY-02, KEY-03, KEY-04, KEY-05, KEY-06, KEY-07]
files_modified:
  - internal/signer/signer.go
  - internal/signer/keyderive.go
  - internal/signer/store.go
  - internal/signer/handler.go
  - internal/signer/store_test.go
  - internal/signer/handler_test.go
  - internal/db/migrations/002_signer_keys.sql
  - internal/db/sqlite_test.go
---

# Phase 3: Persistent Signing and Trust — Summary

## What Was Built

`internal/signer` gives AgentGate a durable Ed25519 signing identity:

- **Generation (KEY-01)**: `LoadOrCreateActive` generates one Ed25519 keypair
  from `crypto/rand` on first persistent startup.
- **Encrypted persistence (KEY-02)**: the private key is encrypted with
  AES-256-GCM under a key derived from the existing vault master secret via
  `HMAC-SHA256(masterKey, "agentgate.signer.v1")` — domain-separated from
  the vault's own token-encryption key, no new dependency.
- **Fail-closed restart (KEY-03)**: a restart with the correct master key
  decrypts and returns the identical key (`TestLoadOrCreateActive_RestartPreservesIdentity`).
  A restart with the wrong master key returns `ErrKeyUnreadable` and leaves
  the persisted row untouched — no silent replacement
  (`TestLoadOrCreateActive_UnreadableKeyFailsClosed`).
- **Deterministic key ID (KEY-05)**: `ComputeKID` returns
  `"ed25519:" + hex(sha256(pub))[:16]`, bound only to public key bytes.
- **Rotation (KEY-06)**: `Rotate(atSeq)` closes the current key's interval at
  `atSeq-1`, activates a new key from `atSeq` onward, and never deletes old
  keys — a message signed before rotation still verifies against the old
  key's public bytes after rotation (`TestRotate_PreservesOldKeyForVerification`).
- **Verifier surface (KEY-04, KEY-07)**: `PublicKeys()` returns
  `[]KeyRecord` with no private-key field in the type at all, and
  `PubkeyHandler` serves it as `GET /v1/receipts/pubkey` JSON. `Verify` and
  `ComputeKID` are pure functions over a public key only — independent of
  signer-side state.

## Scope Boundary Honored

Per PROJECT.md, production composition (mounting the handler into
`cmd/agentgw`, replacing the in-memory vault) is explicitly Phase 4's job
(LEDG-02). This phase delivers only the self-contained package.

## Key Decisions

| Decision | Rationale |
|---|---|
| HMAC-SHA256 purpose-derived key instead of HKDF | Stdlib-only; avoids a new dependency-gate for one subkey extraction |
| New migration `002_signer_keys.sql` alongside the reserved `002_receipts.sql` | Both are `002_`-prefixed and independent; `RunMigrations` applies all files, order doesn't matter between them |
| `kid` format `ed25519:<16 hex chars>` | Fits `Receipt.SignerKID`'s existing 128-byte limit; reproducible from public key bytes alone |
| Partial unique index `WHERE active = 1` | Cross-process backstop; in-process `sync.Mutex` is the primary guard, consistent with the milestone's single-instance scope |

## Test Evidence

- `go test ./internal/signer/... -race -v`: 9/9 pass.
- `go test ./internal/db/... -v`: migration idempotence test passes.
- `go test ./... `: full suite green, no regressions.
- `go vet ./...` and `gofmt -l`: clean.
