# Technology Stack

**Project:** AgentGate Receipts and OSS Launch
**Researched:** 2026-08-11
**Overall confidence:** HIGH, except Biscuit draft alignment, which is MEDIUM

## Recommendation

Keep the existing Go gateway, `database/sql`, raw SQL, SQLite, and Docker Compose path.

Use Go's standard library for receipt hashing, Ed25519 signing, key wrapping, JWK output, and JSONL verification.

Use `github.com/biscuit-auth/biscuit-go/v2` at `v2.2.0` as the only candidate runtime module.

Gate its addition on first-party Biscuit v2 compatibility and graft tests in R7.

Upgrade `github.com/mattn/go-sqlite3` from `v1.14.44` to `v1.14.49` before receipt storage work.

Do not use HMAC for receipts. An HMAC verifier also gains the power to forge receipts.

## Recommended Stack

### Core Framework

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Go module language level | `go 1.25.0` | Existing build contract | Receipt work needs no language upgrade. |
| Go build toolchain | `1.25.12` | Minimum release toolchain | It is the current supported patch for the module line. |
| Go compatibility lane | `1.26.5` | Forward compatibility | It is the current stable release on the research date. |
| `crypto/ed25519` | Go standard library | Receipt signatures | RFC 8032 compatible, constant-time private-key operations, 64-byte signatures. |
| `crypto/sha256` | Go standard library | Parameter, entry, and key-ID digests | Matches the PRD and existing chain reference. |
| `encoding/binary` | Go standard library | Fixed-width canonical encoding | It directly supports explicit little-endian integers. |
| `crypto/x509` | Go standard library | Private-key serialization | PKCS #8 identifies the key algorithm without a custom envelope. |
| `crypto/aes`, `crypto/cipher` | Go standard library | Private-key encryption | Reuses the existing AES-256-GCM custody model. |
| `crypto/hkdf` | Go standard library | Receipt wrapping-key derivation | It separates receipt custody from OAuth token encryption. |
| `crypto/rand` | Go standard library | Key generation and nonces | It is the required cryptographic entropy source. |

Do not change the `go` directive during this milestone.

Build CI and release artifacts with patched toolchains, not bare `1.25.0` images.

### Receipt Canonical Encoding

Define an AgentGate binary format named `agentgate-receipt-v1`.

Do not hash JSON, CBOR, Protobuf, or a Go struct's memory representation.

Use this exact construction:

```text
entry_hash = SHA-256(
  ASCII("AGENTGATE-RECEIPT") || 0x00 || U8(1) ||
  LE64(seq) || LE64(timestamp_unix_ns) ||
  LP(human_principal) || LP(agent_key_id) ||
  LE32(delegation_chain_count) || LP(each delegation_chain item) ||
  LP(service) || LP(action) || params_sha256[32] ||
  LP(policy_decision) || LE32(status_code) || LE64(latency_ms) ||
  LP(error) || prev_hash[32] || LP(signer_kid)
)

signature = Ed25519.Sign(private_key, entry_hash[32])
```

`LP(value)` means `LE32(byte_length) || UTF8(value)`.

Validate every string with `utf8.ValidString` before signing.

Encode `status_code` as an unsigned 32-bit value after range validation.

Encode `latency_ms` as an unsigned 64-bit value after rejecting negatives.

Use 32 zero bytes for the first receipt's `prev_hash`. Start `seq` at 1.

Include `signer_kid` inside `entry_hash`. This binds key selection to signed content.

Do not include `entry_hash` or `signature` inside the hash input.

Do not use varints. Fixed widths avoid alternate encodings of the same value.

Use plain Ed25519. Do not use Ed25519ph or sign the full JSONL line.

### Parameter Digest

Use `encoding/json` v1 with `json.Decoder.UseNumber()` for the existing `params` object.

Marshal the decoded object once. Use those bytes for both request construction and `params_sha256`.

Go v1 sorts map keys during marshaling. Nested maps receive the same deterministic treatment.

Call this normalization `agentgate-params-json-v1`. Do not call it RFC 8785 JCS.

Offline receipt verification checks the stored digest. It does not need the original parameters.

Do not add JCS for receipts. Add it only if AgentGate later implements AAT PoP payloads.

Do not use experimental `encoding/json/v2` while the module stays on Go 1.25.

### Key Custody And Public Format

Generate receipt keys with `ed25519.GenerateKey(rand.Reader)`.

Store private keys as PKCS #8 DER from `x509.MarshalPKCS8PrivateKey`.

Never store a plaintext seed, private JWK, or PEM block in SQLite.

Derive a 32-byte wrapping key with this standard-library call:

```go
hkdf.Key(sha256.New, vaultMasterKey, nil, "agentgate/receipt-key-wrap/v1", 32)
```

Encrypt PKCS #8 DER with AES-256-GCM.

Prefer `cipher.NewGCMWithRandomNonce` for new key custody code.

Use `agentgate/receipt-key/v1:<kid>` as AEAD additional authenticated data.

Persist ciphertext, public key, `kid`, creation time, and retirement time through raw SQL.

Fail startup when persistent custody is configured without the master key.

Never generate a temporary receipt key for a persistent database.

Publish all active and retired public keys as an RFC 7517 JWK Set.

Use `Content-Type: application/jwk-set+json` at `GET /v1/receipts/pubkey`.

Each public JWK should contain these fields:

```json
{
  "kty": "OKP",
  "crv": "Ed25519",
  "alg": "Ed25519",
  "use": "sig",
  "kid": "<RFC-7638-thumbprint>",
  "x": "<base64url-no-padding-32-byte-public-key>"
}
```

Use `alg: "Ed25519"`. RFC 9864 deprecated the polymorphic `EdDSA` JOSE identifier.

Compute `kid` as the base64url SHA-256 JWK thumbprint from RFC 7638.

The thumbprint input is exactly `{"crv":"Ed25519","kty":"OKP","x":"..."}`.

Use `base64.RawURLEncoding` for `x` and `kid`.

Do not add a JOSE package. These fixed JWK structures need only `encoding/json` and `encoding/base64`.

Keep the receipt signing key separate from every Biscuit root key.

### Database

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| SQLite | Driver-bundled release | Receipts, keys, and existing gateway data | Required by the milestone. |
| `database/sql` | Go standard library | Raw SQL transactions and reads | Matches the repository. |
| `github.com/mattn/go-sqlite3` | `v1.14.49` | SQLite driver | Current stable release from 2026-07-29. |

Keep `audit_log`. Add receipt tables through `002_receipts.sql`.

Store hashes, signatures, public keys, and encrypted private keys as BLOB columns.

Store `delegation_chain` as JSON text. Decode it into `[]string` before canonical hashing.

Use SQLite INTEGER only for values at or below `math.MaxInt64`.

Unix nanoseconds fit today. Reject negative or overflowing values before canonical conversion.

Use `seq INTEGER PRIMARY KEY` and fixed-length `CHECK` constraints for BLOB fields.

Use `BEGIN IMMEDIATE` for each append transaction.

With this driver, configure `_txlock=immediate` on the receipt writer connection.

Keep WAL and set `_synchronous=FULL` for durable receipt commits.

Keep `_busy_timeout=5000`. Set the receipt writer pool to one open connection.

Within one transaction, read the head, allocate the next sequence, sign, insert, and commit.

Do not reserve a sequence outside the transaction. A failed insert must not create a gap.

### SQLite Offline Verification

Use the existing driver and a read-only connection.

Open a live database with `mode=ro&_query_only=1`.

Do not set `immutable=1` on a live gateway database.

SQLite warns that changing an immutable file can return stale or corrupt results.

Allow `immutable=1` only for a copied snapshot that cannot change during verification.

Run `PRAGMA integrity_check` first. Treat failure as exit code 2.

Query receipts with an explicit `ORDER BY seq ASC`.

Reject an empty chain unless an explicit `--allow-empty` mode is later required.

Check sequence continuity, previous hashes, entry hashes, known `kid` values, and every Ed25519 signature.

### JSONL Format And Verification

Use newline-delimited JSON with one receipt object per line.

Use media type `application/x-ndjson` for export responses.

Use stable snake-case field names and an explicit `format_version: 1` field.

Encode 32-byte hashes as 64 lowercase hexadecimal characters.

Encode Ed25519 signatures as 128 lowercase hexadecimal characters.

Keep `seq`, timestamps, status codes, and latency as typed JSON numbers.

Decode into a typed struct. Never decode receipt lines into `map[string]any`.

Reject duplicate object names, unknown format versions, malformed UTF-8, and wrong binary lengths.

Use `bufio.Scanner` with an explicit 1 MiB maximum line size.

Use `encoding/json`, `encoding/hex`, `bufio`, and `io`. Add no JSONL package.

Require a separate trusted JWK Set through `--keys keys.jwks`.

An embedded public key can aid lookup. It cannot establish who owns that key.

Do not treat keys inside the receipt export as trust anchors.

Use these exit codes:

| Code | Meaning |
|------|---------|
| `0` | Chain and signatures pass. |
| `1` | Sequence, hash, signature, key, or expected-head mismatch. |
| `2` | Input, schema, key-file, SQLite, or configuration error. |

### Biscuit Attenuation

| Library | Version | Purpose | When To Use |
|---------|---------|---------|-------------|
| `github.com/biscuit-auth/biscuit-go/v2` | `v2.2.0` | Biscuit parsing, verification, authorization, and attenuation | Task R7 only. |

Use the stable `biscuit-auth` module path for `v2.2.0`.

The Eclipse repository moved `main` to `github.com/eclipse-biscuit/biscuit-go/v2` without a newer stable tag.

Do not pin an untagged commit from `main` for launch.

The Eclipse security policy lists `v2.2.0` as supported.

Both `v2.2.0` and the current Go `main` branch emit signature payload version 0.

The current Biscuit specification calls version 0 deprecated.

It requires signature payload version 1 for third-party blocks.

No tagged Go release implements signature payload version 1 or Biscuit v3.

Use `v2.2.0` only for first-party Biscuit v2 attenuation.

If R7 requires v3 or third-party blocks, stop. Do not implement Biscuit cryptography locally.

Run current `govulncheck` after the dependency lands in AgentGate.

Use these APIs directly:

| API | AgentGate use |
|-----|---------------|
| `biscuit.Unmarshal` | Parse serialized token bytes. |
| `(*Biscuit).Authorizer` | Verify the signature chain against a configured root public key. |
| `Authorizer.Authorize` | Enforce ambient service, action, human, and agent facts. |
| `(*Biscuit).CreateBlock` and `Append` | Add attenuation checks. |
| `(*Biscuit).Serialize` | Produce token bytes. |
| `(*Biscuit).BlockCount` | Count appended blocks. Total signed blocks equal this value plus 1. |
| `(*Biscuit).RevocationIds` | Derive stable block identifiers for receipts. |
| `(*Biscuit).Seal` | Prevent further attenuation when policy requires a terminal grant. |

Verify and authorize before using token facts or revocation identifiers.

Encode each `RevocationIds()` value with `base64.RawURLEncoding`.

Store the complete ordered vector in the receipt's `delegation_chain`.

The first identifier binds the authority grant. Later identifiers bind each attenuation block.

Do not store pretty-printed Datalog as the receipt's cryptographic delegation identity.

Use parser parameter substitution. Do not build Datalog by concatenating request strings.

Reject oversized token bytes before `biscuit.Unmarshal`.

Pass `biscuit.WithWorldOptions` with `datalog.WithMaxFacts`, `WithMaxIterations`, and `WithMaxDuration`.

Enforce a separate maximum for `BlockCount()` before authorization.

Test a block graft from token A into token B. Verification must reject the graft.

Also compare the receipt vector against the verified token's block identifiers.

The March 2026 OAuth discussion concerned RFC 8693 token exchange, not a Biscuit defect.

Version 0 links blocks through generated key pairs.

It does not include the previous signature in each appended block's signed payload.

### Infrastructure And OSS Delivery

| Tool | Version or mode | Purpose | Why |
|------|-----------------|---------|-----|
| Apache License | `Apache-2.0` | Source and binary distribution | Required before contributor outreach. |
| GitHub Actions | Hosted native runners | Test and release automation | Native builds avoid CGO cross-compilation traps. |
| `actions/attest` | `v4.2.2` | SLSA provenance for release files and images | Current supported GitHub attestation action. |
| GitHub CLI | Current stable | Release upload and attestation verification | GitHub documents `gh attestation verify`. |
| Docker Buildx | Current stable action, commit-pinned | Multi-architecture GHCR image | Preserves the existing Docker adoption path. |
| `govulncheck` | `golang.org/x/vuln v1.6.0` | Called-vulnerability scanning | Current release from 2026-07-09. |

Commit the full Apache 2.0 `LICENSE` text and a project `NOTICE` file first.

Include `LICENSE` and `NOTICE` in every source archive, binary archive, and container image.

Use `docker compose`, not the retired standalone `docker-compose` command, in launch instructions.

Build CGO binaries on native stable runners:

| Target | Runner |
|--------|--------|
| `linux/amd64` | `ubuntu-24.04` |
| `linux/arm64` | `ubuntu-24.04-arm` |
| `darwin/amd64` | `macos-15-intel` |
| `darwin/arm64` | `macos-15` |

Release both `agentgate` and `agentgate-verify` in each archive.

Generate a SHA-256 checksums file after archives are assembled.

Generate provenance with `actions/attest@v4` using that checksums file.

Pin every third-party action to a full commit SHA in the final workflow.

Publish the container to `ghcr.io/Clawdlinux/agentgate` by digest and semantic tag.

Attest the digest with `actions/attest@v4` and `push-to-registry: true`.

Document these verification commands:

```bash
gh attestation verify agentgate_<version>_linux_amd64.tar.gz \
  --repo Clawdlinux/agentgate

gh attestation verify oci://ghcr.io/Clawdlinux/agentgate:<version> \
  --repo Clawdlinux/agentgate
```

Do not add GoReleaser for the first launch.

GoReleaser documents that CGO cross-compilation needs extra toolchains or its paid split workflow.

The native matrix is smaller, auditable, and sufficient for the launch targets.

## Supporting Libraries

| Library | Version | Purpose | Decision |
|---------|---------|---------|----------|
| `github.com/mattn/go-sqlite3` | `v1.14.49` | Existing SQLite driver | Upgrade and retain. |
| `github.com/biscuit-auth/biscuit-go/v2` | `v2.2.0` | First-party Biscuit v2 attenuation | Add only after the R7 gate. |
| `golang.org/x/vuln/cmd/govulncheck` | `v1.6.0` | CI security tool | Track as a Go tool. |

No other runtime library is required for receipts, verification, custody, or JWK output.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| Receipt signature | Standard `crypto/ed25519` | HMAC-SHA256 | A verifier holding the HMAC key can forge receipts. |
| Receipt signature | Standard `crypto/ed25519` | Third-party Ed25519 package | The standard library already supplies the required primitive. |
| Receipt encoding | Versioned fixed binary | JCS JSON | Receipt verification does not need JSON canonicalization. |
| Receipt encoding | Versioned fixed binary | Canonical CBOR | It adds a format and dependency without solving a current need. |
| Receipt encoding | Versioned fixed binary | Protobuf | Schema tooling adds churn to a fixed local format. |
| Public keys | JWK Set | PEM endpoint | JWK supports key IDs and rotation in a standard JSON form. |
| Key ID | RFC 7638 thumbprint | UUID | A thumbprint is deterministic and tied to the public key. |
| Private key | Encrypted PKCS #8 DER | Plain seed or private JWK | Plain formats expose signing authority. |
| Key custody | Existing master key plus HKDF and AES-GCM | Cloud KMS SDK | It adds vendor code outside the single-tenant milestone. |
| SQLite | `go-sqlite3` | `modernc.org/sqlite` | Switching drivers expands CGO scope and regression risk. |
| JSONL | Standard library | NDJSON package | Line framing and typed JSON need no package. |
| Attenuation | Biscuit Go `v2.2.0` | Macaroons | Macaroons use symmetric chaining and do not meet this public-key design. |
| Attenuation | Stable Biscuit tag | Eclipse `main` pseudo-version | Launch dependencies should use supported tags. |
| Receipt packaging | Raw signed receipt plus JWK | JWS receipt envelope | It duplicates the receipt signature and complicates canonical rules. |
| Release automation | Native GitHub Actions matrix | GoReleaser cross-build | CGO requires extra cross toolchains and release complexity. |
| Supply provenance | GitHub artifact attestations | Custom Cosign workflow | GitHub already signs hosted workflow provenance. |
| Receipt anchoring | Deferred | Rekor checkpoint mirroring | The PRD places this after the milestone. |

## Installation

Apply runtime dependency changes only in their scheduled tasks.

```bash
# R4: current SQLite driver
go get github.com/mattn/go-sqlite3@v1.14.49

# R7: stable Biscuit implementation
go get github.com/biscuit-auth/biscuit-go/v2@v2.2.0

# CI tool tracked by Go 1.24+ tool directives
go get -tool golang.org/x/vuln/cmd/govulncheck@v1.6.0

go mod tidy
go mod verify
go test -race ./...
go vet ./...
go tool govulncheck ./...
```

Do not install a canonical JSON, JOSE, CBOR, Protobuf generator, KMS, or release framework for receipt work.

## Required Verification Fixtures

Add these fixtures before the first format can be called stable:

1. A fixed canonical byte vector with every field populated.
2. A fixed genesis receipt and a two-entry chain.
3. An RFC 8032 Ed25519 test vector.
4. An RFC 7638 Ed25519 JWK thumbprint vector.
5. SQLite and JSONL versions of the same chain.
6. A rotation chain containing two `kid` values.
7. Modified, interior-deleted, inserted, and forged receipt cases.
8. A Biscuit token built from official samples.
9. A Biscuit block-graft rejection case.
10. A clean-machine Docker Compose path ending in offline verification.

## Confidence Assessment

| Area | Confidence | Reason |
|------|------------|--------|
| Ed25519 and key formats | HIGH | Go docs and current RFCs define the APIs and formats. |
| Canonical receipt encoding | HIGH | It extends the existing tested length-prefix construction. |
| SQLite storage and verification | HIGH | Current driver and SQLite docs cover every required mode. |
| JSONL verification | HIGH | The standard library covers strict typed streaming. |
| Biscuit `v2.2.0` | MEDIUM | It is supported, but only emits deprecated signature payload version 0. |
| Biscuit draft alignment | MEDIUM | Current drafts describe related models, not Biscuit compliance. |
| OSS delivery | HIGH | GitHub documents native runners and attestation workflows. |

## Unresolved Facts

### Runtime Custody Wiring

The current command uses `MemoryStore` and `VAULT_ENCRYPTION_KEY`.

The Compose file sets `AGENTGATE_VAULT_KEY`, which the command does not read.

Resolve the owning startup path before generating persistent receipt keys.

### Offline Trust Bootstrap

An export containing its own public key is self-describing, not independently trusted.

Make `--keys` mandatory, or require an explicitly pinned `kid` through another trusted channel.

### Tail Deletion

A hash chain alone cannot detect deletion of its final suffix.

Interior deletion creates a sequence or link failure. Tail truncation leaves an earlier valid head.

Define the acceptance case before R5.

Support `--expected-head <seq>:<hash>` or a trusted signed range manifest if tail detection is required.

External checkpoint publication remains out of scope.

### Draft Status

`draft-niyikiza-oauth-attenuating-agent-tokens-01` is active work in progress from 2026-06-15.

It defines AATs as JWT/JWS chains. It cites Biscuit as related work.

`draft-oauth-ai-agents-on-behalf-of-user-02` expired on 2026-02-27. No `-03` was found.

AgentGate should cite both accurately. It must not claim either draft's compliance.

The AAT draft still says `alg: EdDSA`, which conflicts with RFC 9864.

Use RFC 9864's current `Ed25519` identifier in new AgentGate JWK output.

### Biscuit Module Migration

The supported tag uses the `biscuit-auth` module path.

The Eclipse `main` branch now declares the `eclipse-biscuit` path.

The current Go `main` branch still emits signature payload version 0.

Recheck for a tagged v3-capable release when R7 starts.

Do not change paths or adopt a pseudo-version without that release.

### Legal Authority

Technical research cannot confirm sole-author relicense authority.

Confirm ownership before replacing BSL notices with Apache 2.0 notices.

## Sources

All source checks were current on 2026-08-11.

- [Go `crypto/ed25519`](https://pkg.go.dev/crypto/ed25519). HIGH confidence.
- [Go 1.24 release notes](https://go.dev/doc/go1.24). HIGH confidence.
- [Go release history](https://go.dev/doc/devel/release). HIGH confidence.
- [RFC 8032](https://www.rfc-editor.org/rfc/rfc8032.html). HIGH confidence.
- [RFC 7517](https://www.rfc-editor.org/rfc/rfc7517.html). HIGH confidence.
- [RFC 7638](https://www.rfc-editor.org/rfc/rfc7638.html). HIGH confidence.
- [RFC 9864](https://www.rfc-editor.org/rfc/rfc9864.html). HIGH confidence.
- [IANA JOSE registry](https://www.iana.org/assignments/jose/jose.xhtml). HIGH confidence.
- [go-sqlite3 `v1.14.49`](https://pkg.go.dev/github.com/mattn/go-sqlite3@v1.14.49). HIGH confidence.
- [SQLite transactions](https://sqlite.org/lang_transaction.html). HIGH confidence.
- [SQLite URI filenames](https://sqlite.org/uri.html). HIGH confidence.
- [SQLite PRAGMAs](https://sqlite.org/pragma.html). HIGH confidence.
- [Eclipse Biscuit Go repository](https://github.com/eclipse-biscuit/biscuit-go). HIGH confidence for API ownership.
- [Eclipse Biscuit Go usage](https://doc.biscuitsec.org/usage/go). HIGH confidence for tagged API use.
- [Eclipse Biscuit specification](https://doc.biscuitsec.org/reference/specifications). HIGH confidence for current token semantics.
- [Biscuit 3.0 compatibility note](https://www.biscuitsec.org/blog/biscuit-3-0/). HIGH confidence for v2 and v3 scope.
- [AAT draft `-01`](https://www.ietf.org/archive/id/draft-niyikiza-oauth-attenuating-agent-tokens-01.html). MEDIUM confidence because it is work in progress.
- [On-behalf-of-user draft `-02`](https://www.ietf.org/archive/id/draft-oauth-ai-agents-on-behalf-of-user-02.html). LOW confidence for future direction because it expired.
- [OAuth chain-splicing discussion](https://mailarchive.ietf.org/arch/msg/oauth/6MHkSfhGfugVmcb2p08ocM7piqQ/). MEDIUM confidence as working-group discussion.
- [GitHub-hosted runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners). HIGH confidence.
- [GitHub artifact attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations). HIGH confidence.
- [GoReleaser CGO limitation](https://goreleaser.com/resources/limitations/cgo/). HIGH confidence.
- [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0). HIGH confidence.
