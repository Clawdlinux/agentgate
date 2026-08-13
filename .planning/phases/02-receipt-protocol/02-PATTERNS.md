# Phase 2: Receipt Protocol - Pattern Map

**Mapped:** 2026-08-13
**Files analyzed:** 13 expected new or modified artifacts
**Analogs found:** 9 / 13

## Scope Rules

Phase 2 owns pure receipt protocol primitives and immutable fixtures.

Do not wire receipts into `internal/gateway` yet.
Do not sign, persist, export, or sequence receipts here.
Do not retain raw or canonical parameter JSON.

## File Classification

| New or Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/receipt/receipt.go` | model, validation | transform | `internal/auth/keys.go` and `internal/vault/vault.go` | role-match |
| `internal/receipt/params.go` | utility, validation | transform | `internal/gateway/gateway.go` as an anti-analog | partial |
| `internal/receipt/encoding.go` | utility | transform | `../agentic-operator-core/pkg/audit/chain.go` | exact |
| `internal/receipt/receipt_test.go` | test | transform | `internal/vault/vault_test.go` | role-match |
| `internal/receipt/params_test.go` | test | transform | `internal/vault/vault_test.go` | role-match |
| `internal/receipt/encoding_test.go` | test | transform | `../agentic-operator-core/pkg/audit/audit_test.go` | exact |
| `internal/receipt/receipt_external_test.go` | black-box test | transform | `../agentic-operator-core/pkg/audit/audit_test.go` | exact |
| `internal/receipt/fuzz_test.go` | fuzz test | transform | None in AgentGate | none |
| `internal/receipt/testdata/v1/manifest.json` | fixture config | file-I/O | None in AgentGate | none |
| `internal/receipt/testdata/v1/genesis-unicode-max.bin` | binary fixture | file-I/O | None in AgentGate | none |
| `internal/receipt/testdata/v1/gen/main.go` | fixture generator | file-I/O | None in AgentGate | none |
| `go.mod` | dependency config | build-time | Existing direct dependency block | role-match |
| `go.sum` | dependency lock | build-time | Existing module and module-file checksum pairs | role-match |

## Pattern Assignments

### `internal/receipt/receipt.go`

**Role:** Model and strict validation.

**Analogs:** `internal/auth/keys.go` and `internal/vault/vault.go`.

Use the Apache header from `internal/vault/vault.go` lines 1-4.

```go
/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/
```

Use package-scoped sentinel errors like `internal/auth/keys.go` lines 16-19.

```go
var (
    ErrKeyNotFound = errors.New("auth: key not found")
    ErrKeyRevoked  = errors.New("auth: key revoked")
)
```

Receipt errors should use the `receipt:` prefix.
Keep public errors stable and free of field contents.
Use `errors.Is` compatible sentinels for testable categories.

Use constructor-style input checks like `internal/vault/vault.go` lines 66-78.

```go
func NewMemoryStore(encryptionKey []byte) (*MemoryStore, error) {
    if len(encryptionKey) != 32 {
        return nil, fmt.Errorf("vault: encryption key must be 32 bytes, got %d", len(encryptionKey))
    }
    // Construction continues only after validation.
}
```

`Validate` should check every field before encoding.
It must not clamp values or normalize strings.
Limits apply to UTF-8 bytes, not rune counts.

Copy `Receipt` before validation.
Clone `DelegationChain` with `slices.Clone`.
This prevents later caller mutation from changing one operation's snapshot.

Keep fields in the exact protocol order.
Keep derived fields in the type but exclude them from hash input.

```go
type Receipt struct {
    Seq               uint64
    TimestampUnixNS   uint64
    HumanPrincipal    string
    AgentKeyID        string
    DelegationChain   []string
    Service           string
    Action            string
    ParamsSHA256      [32]byte
    PolicyDecision    string
    StatusCode        int
    LatencyMS         int64
    Error             string
    PrevHash          [32]byte
    EntryHash         [32]byte
    SignerKID         string
    Signature         [64]byte
}
```

`PolicyDecision` accepts only `allow`, `deny`, or `rate_limited`.
`Error` accepts empty or `^[a-z0-9_]{1,64}$`.
Delegation elements and error codes are ASCII only.
Other bounded strings require valid UTF-8 and reject NUL.

Do not add constructors that sign or persist.
The public API stays centered on pure functions.

---

### `internal/receipt/params.go`

**Role:** Strict raw JSON validation and RFC 8785 digesting.

**Anti-analog:** `internal/gateway/gateway.go` lines 73-78.

Do not copy its map-first decode. It collapses duplicate names. `DigestParams` accepts raw `[]byte`.

Normalize nil, JSON whitespace, and exact `null` to `{}`. Validate `utf8.Valid` before decoding. Explicitly validate escaped surrogate pairs in JSON strings. Reject lone high or low surrogates, reversed pairs, broken adjacency, and malformed `\u` escapes. Accept valid pairs.

Use `encoding/json.Decoder` with `UseNumber`. Walk tokens with a container stack. Require one object root, depth at most 32, and EOF. Each object frame keeps a decoded-name set. This rejects literal duplicates and escaped equivalents before map collapse.

Reject Unicode noncharacters in decoded names and values. Parse every `json.Number` with `strconv.ParseFloat(..., 64)` and reject range errors. The validation layer must not canonicalize or rewrite strings or numbers.

Call `jcs.Transform` only after validation. It performs all RFC 8785 string escaping, number formatting, and property sorting. Reject canonical output above `1 << 20` bytes. Return only `sha256.Sum256(canonical)`.

The candidate parser does not validate raw UTF-8. Its surrogate behavior requires AgentGate tests and explicit pre-validation. Map all parser and dependency failures to stable local errors. Never expose dependency error text.

Expected imports keep standard library and external dependency groups separate.

```go
import (
    "bytes"
    "crypto/sha256"
    "encoding/json"
    "io"
    "strconv"
    "unicode/utf8"

    "github.com/gowebpki/jcs"
)
```

---

### `internal/receipt/encoding.go`

**Role:** Canonical preimage and entry hash.

**Analog:** `../agentic-operator-core/pkg/audit/chain.go` lines 187-215.

Copy fixed little-endian encoding and uint32 length prefixes.

```go
func computeEntryHash(e *Entry) [32]byte {
    h := sha256.New()
    var buf [8]byte

    binary.LittleEndian.PutUint64(buf[:], e.Seq)
    h.Write(buf[:])

    binary.LittleEndian.PutUint64(buf[:], e.TimestampUnixN)
    h.Write(buf[:])
    // Length-prefixed fields and fixed hashes follow.
}

func writeLP(h interface{ Write(p []byte) (int, error) }, b []byte) {
    var buf [4]byte
    binary.LittleEndian.PutUint32(buf[:], uint32(len(b)))
    _, _ = h.Write(buf[:])
    _, _ = h.Write(b)
}
```

Prefer Go 1.25 append APIs for the new implementation.
They make returned canonical bytes explicit.

```go
func appendLP(dst []byte, value string) []byte {
    dst = binary.LittleEndian.AppendUint32(dst, uint32(len(value)))
    return append(dst, value...)
}
```

Start every preimage with these exact bytes.

```go
const HashDomainV1 = "agentgate.receipt.hash.v1"

out := append([]byte(HashDomainV1), 0)
```

Append fields in the locked order from `02-CONTEXT.md` D-03.
Use 8-byte little-endian for both unsigned fields.
Use 4-byte little-endian for validated `status_code`.
Use 8-byte little-endian for validated `latency_ms`.
Append both 32-byte hashes without a length prefix.

`CanonicalHashInput` must validate its private snapshot.
It returns a fresh byte slice on every successful call.

`ComputeEntryHash` should call `CanonicalHashInput`.
Then return `sha256.Sum256(preimage)`.

Do not mutate `EntryHash`, `Signature`, or any caller field.
Do not include `EntryHash` or `Signature` in the preimage.
Do include `SignerKID`.

---

### `internal/receipt/receipt_test.go`

**Role:** Field contract, validation boundaries, and snapshot behavior.

**Analog:** `internal/vault/vault_test.go` lines 110-133.

Copy the table-driven subtest shape.

```go
cases := []struct {
    name    string
    tok     Token
    expired bool
}{
    {"no expiry", Token{}, false},
    {"future", Token{ExpiresAt: time.Now().Add(time.Hour)}, false},
    {"past", Token{ExpiresAt: time.Now().Add(-time.Hour)}, true},
}
for _, tc := range cases {
    tc := tc
    t.Run(tc.name, func(t *testing.T) {
        t.Parallel()
        // One focused assertion.
    })
}
```

Add a reflection test for exact field names, order, and types.
Use one valid receipt helper, then mutate one field per case.
Test every lower and upper boundary from the validation matrix.

Test byte limits with multibyte UTF-8.
Test exact-byte preservation with unnormalized Unicode.
Test NUL rejection separately from invalid UTF-8.
Test empty delegation elements as valid.

For snapshot tests, mutate the caller's delegation slice after a call.
The previously returned canonical bytes must remain unchanged.

---

### `internal/receipt/params_test.go`

**Role:** Strict JCS behavior, limits, and privacy.

**Analogs:** `internal/vault/vault_test.go` and `internal/gateway/gateway_test.go`.

Use table tests with input, expected digest, and expected error category.
Use `errors.Is`, as shown in `internal/vault/vault_test.go` lines 51-59.

```go
_, err := store.Get("nobody", "nothing")
if !errors.Is(err, ErrTokenNotFound) {
    t.Fatalf("expected ErrTokenNotFound, got %v", err)
}
```

Cover equivalent whitespace and object key order.
Cover RFC 8785 number and string vectors.
Cover escaped duplicate names and literal duplicate names.
Cover invalid UTF-8, malformed surrogates, and noncharacters.
Cover depths 32 and 33.
Cover canonical sizes at 1 MiB and one byte above.
Cover `9007199254740992`, maximum binary64, and `1e309`.
Cover nil, empty, whitespace, `null`, arrays, scalars, and trailing JSON.

Copy the privacy sentinel style from `internal/vault/vault_test.go` lines 93-106.

```go
for i := 0; i < len(raw)-17; i++ {
    if string(raw[i:i+18]) == "secret-token-value" {
        t.Fatal("plaintext access token found in stored bytes")
    }
}
```

Use `bytes.Contains` for the new tests.
Scan receipt state, canonical preimages, and returned errors.
Sentinels must include raw parameters, tokens, bodies, and provider text.

---

### `internal/receipt/encoding_test.go`

**Role:** Binary contract, hash behavior, and derived-field exclusion.

**Analog:** `../agentic-operator-core/pkg/audit/audit_test.go` lines 113-203.

Copy the one-mutation-per-test approach.
That suite changes payloads, previous hashes, signatures, and sequences independently.

```go
be.Tamper(2, func(e *audit.Entry) {
    e.PrevHash = fake
})

if rep.FirstError == nil {
    t.Fatal("expected tamper detection")
}
```

For Phase 2, mutate each included field.
Assert canonical bytes change.
Mutate `EntryHash` and `Signature`.
Assert canonical bytes stay equal.

Assert the exact domain and trailing NUL.
Assert field order, widths, lengths, and little-endian values.
Assert zero `PrevHash` remains 32 zero bytes.
Assert `ComputeEntryHash` equals `sha256.Sum256(CanonicalHashInput(r))`.

Use deterministic values only.
Do not use random keys or wall-clock time.

---

### `internal/receipt/receipt_external_test.go`

**Role:** Independent black-box reference encoder and golden checks.

**Analog:** `../agentic-operator-core/pkg/audit/audit_test.go` lines 6-18.

Copy the external test package convention.

```go
package audit_test

import (
    "testing"

    "github.com/Clawdlinux/agentic-operator-core/pkg/audit"
)
```

Use `package receipt_test`.
Import `github.com/Clawdlinux/agentgate/internal/receipt`.

The reference encoder may read public fields and protocol constants.
It must append every field independently.
It must not call these production functions:

- `Validate`
- `CanonicalHashInput`
- `ComputeEntryHash`
- Any private production helper

Compare reference bytes to production bytes before comparing hashes.
Load fixed fixture files read-only.
Never update them from tests.

---

### `internal/receipt/fuzz_test.go`

**Role:** Parser and binary property fuzzing.

**Analog:** None in AgentGate.

Use standard Go fuzz functions.
Do not add a fuzzing dependency.

```go
func FuzzDigestParams(f *testing.F) {
    for _, seed := range digestSeeds {
        f.Add(seed)
    }
    f.Fuzz(func(t *testing.T, raw []byte) {
        // Never panic. Repeated input must return the same result category.
    })
}
```

Add `FuzzDigestParams` and `FuzzCanonicalHashInput`.
Seed every deterministic boundary before random mutation.

Fuzz properties must avoid flaky assumptions.
Malformed input may return any documented stable category.
Successful repeated calls must return equal results.
Returned canonical slices must not alias caller slices.

Use the commands from `02-VALIDATION.md`.
Each fuzzer runs for 10 seconds at the phase gate.

---

### `internal/receipt/testdata/v1/manifest.json`

**Role:** Human-readable fixture contract.

**Analog:** None in AgentGate.

Use stable JSON field order and 2-space indentation.
Include protocol version, domain, receipt fields, binary filename, length, and SHA-256.
Describe invalid boundary cases without binary files.

Store byte arrays and hashes as lowercase hexadecimal strings.
Store maximum integers as decimal strings if JSON number precision is ambiguous.
Do not include raw parameter JSON, tokens, bodies, or provider errors.

The manifest is reviewed protocol data.
Tests must never rewrite it.

---

### `internal/receipt/testdata/v1/genesis-unicode-max.bin`

**Role:** Immutable canonical hash preimage.

**Analog:** None in AgentGate.

The file contains only canonical preimage bytes.
It is not JSONL and not a serialized receipt.

The fixture must cover:

- Maximum sequence, timestamp, and latency.
- Status 599.
- Zero genesis `PrevHash`.
- Empty delegation chain.
- Exact unnormalized multibyte UTF-8.
- Nonzero derived fields in the manifest to prove exclusion.

Document its byte length and SHA-256 in the manifest.
Never regenerate it during `go test`.

---

### `internal/receipt/testdata/v1/gen/main.go`

**Role:** Explicit, no-overwrite fixture generator.

**Analog:** None in AgentGate.

Use `package main` under `testdata`.
The generator may use the independent encoding implementation.
It must not import private production helpers.

Create outputs with `os.O_CREATE|os.O_EXCL|os.O_WRONLY`.
Use mode `0o644`.
Fail when any destination already exists.

Do not add `-update`, `UPDATE_GOLDEN`, or overwrite behavior.
Do not invoke the generator from tests or `go generate`.
Fixture replacement requires a protocol version change and review.

---

### `go.mod`

**Role:** Direct dependency declaration.

Keep the existing two-block layout from `go.mod` lines 5-15.
Add the pinned parser to the direct dependency block.

```go
require (
    github.com/gowebpki/jcs v1.0.1
    github.com/mattn/go-sqlite3 v1.14.44
    gopkg.in/yaml.v3 v3.0.1
)
```

Do not use `latest`.
Do not adopt a revision requiring Go 1.26.
Keep `go 1.25.0` unchanged.

The dependency command runs only after the SkillSpector gate passes.

---

### `go.sum`

**Role:** Dependency checksum lock.

Follow the existing pair convention in `go.sum` lines 1-10.
Each dependency has module and `go.mod` checksums.

Expected module checksum from research:

```text
github.com/gowebpki/jcs v1.0.1 h1:Qjzg8EOkrOTuWP7DqQ1FbYtcpEbeTzUoTN9bptp8FOU=
github.com/gowebpki/jcs v1.0.1/go.mod h1:CID1cNZ+sHp1CCpAR8mPf6QRtagFBgPJE0FCUQ6+BrI=
```

Let the Go tool write the matching `go.mod` checksum.
Do not hand-author or delete unrelated checksum lines.
Verify the pinned source and checksum before acceptance.

## Shared Patterns

### Error Handling

Use stable `receipt:` sentinel errors for caller-visible categories.
Use `fmt.Errorf("receipt.Function: %w", err)` for internal wrapped failures.

Do not expose parser error text.
Do not include rejected field values in errors.
This differs from older vault errors that include user and service identifiers.

### Validation Order

Normalize allowed empty parameter inputs first.
Then parse, validate semantics, canonicalize, check size, and hash.

For receipts, snapshot first.
Then validate every field before narrowing integers or appending bytes.

### Purity And Ownership

Public receipt operations return values and errors.
They do not mutate callers or package-global state.
They perform no database, HTTP, logging, clock, or file operations.

### Test Style

Use standard `testing` only.
Use table-driven cases and named subtests.
Use `t.Helper` for fixture constructors.
Use `t.Parallel` only when no shared mutable state exists.

Use `package receipt_test` only for the independent encoder file.
Keep white-box boundary tests in `package receipt`.

### Privacy Checks

Use distinct sentinels for each forbidden class.
Search all returned bytes, errors, and manifest text.
A passing hash is insufficient if error text leaks input.

### SkillSpector Dependency Gate

Verify the committed SAFE report before `go get`.

1. Require module `github.com/gowebpki/jcs` and tag `v1.0.1`.
2. Require commit `1a4242a66e1a8e03d7458324d0bc95c327527cbb`.
3. Require SkillSpector 2.8.2 static `SAFE`, score 3, and 0 findings.
4. Verify Apache-2.0 and the candidate own tests.
5. Verify the exact module Sum and GoModSum.
6. Run `go get github.com/gowebpki/jcs@v1.0.1`.

The historical `go-json-experiment` result remains `DO_NOT_INSTALL`. Never install it. LLM analysis remains disabled.

### Validation Commands

```bash
go test ./internal/receipt -count=1
go test -race ./internal/receipt -count=1
go test ./...
go vet ./...
go test ./internal/receipt -run '^$' -fuzz '^FuzzDigestParams$' -fuzztime=10s
go test ./internal/receipt -run '^$' -fuzz '^FuzzCanonicalHashInput$' -fuzztime=10s
git diff --check
```

## Reference Patterns Not To Copy

### HMAC From `chain.go`

Do not copy `crypto/hmac`, signing keys, `Hash` mutation, or signature checks.
Phase 2 computes SHA-256 entry hashes only.
Phase 3 uses Ed25519 for independent verification.

`chain.go` lines 148-152 and 176-181 are prohibited patterns.
HMAC gives verifiers forging capability.

### Payload Retention From `chain.go`

Do not copy `PayloadCanon []byte` from the reference `Entry`.
AgentGate stores only `ParamsSHA256 [32]byte`.
Canonical parameter JSON must not enter `Receipt`.

### Async Audit Logging

Do not copy `internal/audit/logger.go` lines 28-51.
It uses a buffered channel and drops entries when full.

```go
select {
case l.ch <- e:
default:
    l.logger.Warn("audit: buffer full, dropping entry")
}
```

Do not copy `time.Sleep` synchronization from `logger_test.go` line 61.
Phase 2 has no logging.
Phase 4 will implement synchronous, gap-free persistence.

### Gateway Map Decoding

Do not copy `map[string]interface{}` parameter decoding.
It loses duplicate-name evidence.
Do not modify the gateway during Phase 2.

### Global Clock Seam

Do not copy `audit.Now` from `chain.go` lines 217-218.
Phase 2 receives timestamps as receipt data.
Pure protocol functions do not read the clock.

### In-Place Hash Mutation

Do not copy `ChainHasher.Hash(e *Entry)`.
It mutates payload hashes, signer IDs, entry hashes, and signatures.
Phase 2 returns computed values without changing `Receipt`.

## No Analog Found

| File | Reason | Planner Direction |
|---|---|---|
| `internal/receipt/fuzz_test.go` | AgentGate has no fuzz tests. | Use standard Go fuzzing and research seeds. |
| `internal/receipt/testdata/v1/manifest.json` | AgentGate has no testdata fixtures. | Treat it as reviewed protocol data. |
| `internal/receipt/testdata/v1/genesis-unicode-max.bin` | AgentGate has no binary goldens. | Check fixed length and SHA-256. |
| `internal/receipt/testdata/v1/gen/main.go` | AgentGate has no fixture generator. | Use exclusive creation and no overwrite path. |

## Metadata

**Analog search scope:** `internal/gateway`, `internal/audit`, `internal/auth`, `internal/vault`, and cross-repo `pkg/audit`.

**Primary source analogs:** 10 files.

**Expected artifacts classified:** 13.

**Pattern extraction date:** 2026-08-13.
