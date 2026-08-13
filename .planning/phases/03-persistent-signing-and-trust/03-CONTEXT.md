---
phase: 03-persistent-signing-and-trust
gathered: 2026-08-14
status: ready-for-planning
mode: autonomous (autopilot loop, no interactive user available)
---

<domain>
## Phase Boundary

Give AgentGate a durable Ed25519 signer identity with encrypted-at-rest private
key material, deterministic key IDs, rotation bound to receipt sequence
intervals, and a public key history an independent verifier can trust without
gateway state. Requirements: KEY-01 through KEY-07.

This phase does NOT wire the signer into `cmd/agentgw` production
composition. PROJECT.md already assigns that wiring to Phase 4
(LEDG-02: "Production uses persistent SQLite auth, vault, migrations, scopes,
and receipt dependencies before vault access"). Phase 3 delivers a
self-contained, independently testable `internal/signer` package and an
HTTP handler function for `GET /v1/receipts/pubkey` that Phase 4 composes
into the running server.

</domain>

<decisions>
## Implementation Decisions

### Purpose-derived key (KEY-02)
Reuse the existing `AGENTGATE_VAULT_KEY` / `VAULT_ENCRYPTION_KEY` 32-byte
master secret already used by `internal/vault`. Derive a distinct 32-byte
AES-256 key for signer storage via `HMAC-SHA256(masterKey, "agentgate.signer.v1")`.
This keeps the signer's encryption key cryptographically separate from the
vault's token encryption key while requiring no new dependency (stdlib
`crypto/hmac` + `crypto/sha256` only — HMAC-SHA256 is a standard,
well-analyzed key-derivation primitive for this single-subkey case).
Rejected: golang.org/x/crypto/hkdf — would require the same dependency-gate
process as Phase 2's jcs pin for no material benefit at this key count.

### Storage (KEY-01, KEY-02, KEY-03)
New migration `002_signer_keys.sql` (the reserved name `002_receipts.sql`
belongs to Phase 4's LEDG-01; both can coexist as separate `002_`-prefixed
files, applied in alphabetical order by `db.RunMigrations`).

```sql
CREATE TABLE IF NOT EXISTS signer_keys (
    kid             TEXT PRIMARY KEY,
    public_key      BLOB NOT NULL,
    private_key_enc BLOB NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    valid_from_seq  INTEGER NOT NULL,
    valid_until_seq INTEGER,
    active          INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_signer_keys_one_active
    ON signer_keys(active) WHERE active = 1;
```

First persistent startup: no active row exists, so the store generates one
Ed25519 keypair from `crypto/rand`, encrypts the private key, and inserts it
with `valid_from_seq = 1`, `valid_until_seq = NULL`, `active = 1`.

Restart: an active row exists. The store decrypts it. If decryption fails
(wrong master key, corrupted ciphertext), the store returns a hard error and
the caller must fail startup — it must never silently generate a
replacement key, which would break every existing receipt chain (KEY-03).

### Deterministic key ID (KEY-05)
`kid = "ed25519:" + hex(sha256(publicKeyBytes))[:16]` — bound only to the
public key bytes, reproducible by anyone who has the public key, and fits
`Receipt.SignerKID`'s existing 128-byte UTF-8 limit with room to spare.

### Rotation and sequence intervals (KEY-06)
`Rotate(atSeq)` runs in one transaction: sets the current active row's
`valid_until_seq = atSeq - 1`, deactivates it (`active = 0`), generates a new
Ed25519 keypair, and inserts it with `valid_from_seq = atSeq`,
`valid_until_seq = NULL`, `active = 1`. `atSeq` must be greater than the
current active key's `valid_from_seq` or the call fails — this keeps
intervals non-overlapping and non-empty. Old keys are never deleted, so a
receipt signed at any historical sequence remains verifiable against the
key whose interval contains that sequence.

### Verifier-facing surface (KEY-04, KEY-07)
`PublicKeys()` returns `[]KeyRecord` (KID, public key, created-at, valid
sequence interval) with no private-key field in the type at all — there is
no accidental-leak path through JSON marshaling. `PubkeyHandler` wraps this
in a `GET /v1/receipts/pubkey` handler that Phase 4 mounts. `Verify(pub,
message, sig)` and `ComputeKID(pub)` are pure functions requiring only a
public key — an independent verifier needs no signer-side state, matching
KEY-07.

### Concurrency
A `sync.Mutex` serializes `LoadOrCreateActive` and `Rotate` within one
process (mirrors `internal/vault`'s `sync.RWMutex` pattern). The partial
unique index (`WHERE active = 1`) is the cross-process/last-resort
correctness backstop, consistent with this milestone's single-tenant,
single-instance scope (PROJECT.md Out of Scope: multi-tenant isolation).

</decisions>

<code_context>
## Existing Code Insights

- `internal/vault/vault.go` and `sqlite_store.go`: AES-256-GCM, 32-byte key
  required, `crypto/rand` nonce per encrypt call, license header + package
  doc comment convention, plain stdlib crypto (no third-party crypto deps).
- `internal/db/sqlite.go` + `migrations/001_init.sql`: `RunMigrations` embeds
  `migrations/*.sql` and execs each file in `ReadDir` order (alphabetical).
  Only `001_init.sql` exists today — `agent_keys`, `tokens`, `audit_log`.
- `internal/receipt/receipt.go`: `SignerKID` is a free-form UTF-8 string,
  ≤128 bytes, no format constraint yet — Phase 3's `kid` format is free to
  define here without breaking Phase 2's contract.
- `cmd/agentgw/main.go`: currently uses `vault.NewMemoryStore` with a
  `VAULT_ENCRYPTION_KEY` env var (falls back to random key + warning if
  unset). This in-memory composition is explicitly Phase 4 scope to replace
  with persistent SQLite wiring — Phase 3 must not touch `main.go`.
- `.env.example`: master secret env var is actually named
  `AGENTGATE_VAULT_KEY` in the example file, while `main.go` reads
  `VAULT_ENCRYPTION_KEY`. This mismatch predates this phase and belongs to
  Phase 4's production-wiring cleanup, not Phase 3.

</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above — ROADMAP.md's Phase 3 success criteria and
KEY-01 through KEY-07 are the spec.

</specifics>

<deferred>
## Deferred Ideas

- Wiring `PubkeyHandler` into `cmd/agentgw`'s router: Phase 4.
- Fixing the `AGENTGATE_VAULT_KEY` vs `VAULT_ENCRYPTION_KEY` naming mismatch: Phase 4.
- KMS-backed master key (vs. env var): out of scope per PRD, no milestone task covers it.

</deferred>
