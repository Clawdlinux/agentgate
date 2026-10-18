# AgentGate receipt v1

Status: frozen from `origin/main` at `f42a5a80b9937a5bbcad9013cb1dbd3baa883b1e`.
This document describes that implementation, including its permissive parsing and verification limits.
It does not change production behavior.

The protocol implementation is [`pkg/receiptspec`](../../pkg/receiptspec).
Gateway params hashing lives in [`internal/receipt`](../../internal/receipt).
The standalone verifier is [`cmd/agentgate-verify`](../../cmd/agentgate-verify).

## 1. Values and limits

Lengths below count bytes, not Unicode characters.
Required strings contain valid UTF-8, contain no NUL byte, and are nonempty.
Strings are not Unicode-normalized.

| Field | Semantic type | Accepted values |
|---|---|---|
| `seq` | unsigned 64-bit integer | `1` through `18446744073709551615` |
| `timestamp_unix_ns` | unsigned 64-bit integer | `1` through `18446744073709551615` |
| `human_principal` | required string | At most 256 bytes |
| `agent_key_id` | required string | At most 128 bytes |
| `delegation_chain` | ordered string list | At most 32 elements |
| Each delegation element | ASCII string | 0 through 64 bytes; no NUL |
| `service` | required string | At most 64 bytes |
| `action` | required string | At most 128 bytes |
| `params_sha256` | 32 bytes | Any byte value |
| `policy_decision` | string enum | Exactly `allow`, `deny`, or `rate_limited` |
| `status_code` | integer | 100 through 599, inclusive |
| `latency_ms` | signed 64-bit integer | 0 through `9223372036854775807` |
| `error` | string | Empty, or 1 through 64 ASCII bytes matching `[a-z0-9_]+` |
| `prev_hash` | 32 bytes | Any byte value; linkage adds constraints |
| `entry_hash` | 32 bytes | SHA-256 of the receipt preimage |
| `signer_kid` | required string | At most 128 bytes |
| `signature` | 64 bytes | Ed25519 signature |

`timestamp_unix_ns` represents nanoseconds since the Unix epoch.
Validation requires only a nonzero value.
It imposes no timestamp ordering, clock-skew limit, or calendar range.
`status_code`, `error`, and `policy_decision` have no cross-field consistency checks.
Error codes have a character rule, not a closed vocabulary.
Provider error text does not belong in this field.

`Validate` returns `ErrInvalidReceipt` for invalid numbers.
It returns `ErrInvalidField` for invalid strings or delegation lists.
It does not authenticate hashes or signatures.
The receipt verifier does not recover params from their hash.

## 2. Receipt hash preimage

Define `U32LE(x)` and `U64LE(x)` as unsigned little-endian integers occupying 4 and 8 bytes.
Define `LP(s) = U32LE(len(UTF8(s))) || UTF8(s)`.
`||` means byte concatenation.
There is no padding or alignment.
Only the domain string has a terminating NUL.

`CanonicalHashInput` validates the receipt, then concatenates these fields in this exact order:

| Order | Input | Encoding |
|---|---|---|
| 1 | Domain | ASCII `agentgate.receipt.hash.v1`, then `00`; 26 bytes total |
| 2 | `seq` | `U64LE(seq)` |
| 3 | `timestamp_unix_ns` | `U64LE(timestamp_unix_ns)` |
| 4 | `human_principal` | `LP(human_principal)` |
| 5 | `agent_key_id` | `LP(agent_key_id)` |
| 6 | Delegation element count | `U32LE(count)` |
| 7 | Delegation elements | `LP(element)` for each element, in list order |
| 8 | `service` | `LP(service)` |
| 9 | `action` | `LP(action)` |
| 10 | `params_sha256` | 32 raw bytes; no length prefix |
| 11 | `policy_decision` | `LP(policy_decision)` |
| 12 | `status_code` | Convert to `int32`, then `uint32`, then `U32LE` |
| 13 | `latency_ms` | Convert to `uint64`, then `U64LE` |
| 14 | `error` | `LP(error)`; an empty error contributes `00000000` |
| 15 | `prev_hash` | 32 raw bytes; no length prefix |
| 16 | `signer_kid` | `LP(signer_kid)` |

`entry_hash = SHA256(CanonicalHashInput(receipt))`.
Neither `entry_hash` nor `signature` appears in this preimage.
`signer_kid` and `prev_hash` do appear.
The receipt's JSON `format_version` and `type` do not appear.
The domain selects the binary version.
Valid maximum-length fields produce at most 3106 preimage bytes.
Nil and empty delegation lists both contribute a zero element count.
An empty delegation element contributes its own 4-byte zero length.

## 3. Params SHA-256

### 3.1 Gateway params

`DigestParams` and `ParseAndDigestParams` implement the gateway rule.
The receipt contains only the resulting 32-byte SHA-256 digest.

1. Trim leading and trailing whitespace with Go `bytes.TrimSpace`.
2. Normalize empty input and the exact trimmed token `null` to `{}`.
3. Require valid UTF-8 and valid escaped surrogate pairs.
4. Decode exactly 1 JSON object with `json.Decoder.UseNumber`.
5. Reject trailing JSON values or invalid syntax.
6. Reject duplicate decoded object names at every nesting level.
7. Reject excessive nesting and invalid string or number values.
8. Serialize the decoded object with the rules below.
9. Reject canonical output larger than `1048576` bytes.
10. Return SHA-256 of the canonical bytes.

The top-level object counts as depth 1.
Each nested object or array adds 1.
Depth 32 is accepted. Depth 33 is rejected.
Scalars do not add a nesting level.
There is no raw-input size check inside these functions.
The size check applies after canonicalization.

Object names sort lexicographically by unsigned UTF-16 code units.
Array order is preserved.
Whitespace outside strings is removed.
`null`, `true`, and `false` retain those lowercase spellings.
Decoded strings retain their Unicode scalar sequence without normalization.
Escape `"` and `\` with a backslash.
Use `\b`, `\t`, `\n`, `\f`, and `\r` for those control characters.
Other U+0000 through U+001F characters use lowercase `\u00xx`.
Other characters remain literal UTF-8.
In particular, `/`, `<`, `>`, `&`, U+2028, and U+2029 are not escaped.

Both names and string values reject Unicode noncharacters.
These are U+FDD0 through U+FDEF and each plane's U+FFFE and U+FFFF.
Lone or reversed surrogate escapes are rejected.
Proper high-surrogate and low-surrogate pairs are accepted.
Escaped NUL is accepted in params strings.
Names such as `"a"` and `"\u0061"` are duplicates.
Composed `é` and decomposed `e\u0301` remain distinct.

Numbers follow the implemented RFC 8785 binary64 path.
Each numeric token passes through Go `strconv.ParseFloat(text, 64)`.
Conversion errors, infinities, and NaN are rejected.
Accepted values use `github.com/gowebpki/jcs` v1.0.1 `NumberToJSON`.
This uses ECMAScript's shortest round-trippable binary64 decimal representation.
Both signs of zero become `0`.
Nonzero absolute values from `1e-6` inclusive to `1e21` exclusive use ordinary decimal notation.
Other nonzero values use lowercase `e` notation, without exponent zero padding.
Positive exponents include `+`.
Fractions and exponent notation are accepted.
Integers outside the exact binary64 range can round.
Small values can underflow to zero without a conversion error.
There is no safe-integer restriction for gateway params.

Failures return `ErrInvalidParams`, except excessive canonical size returns `ErrParamsTooLarge`.
These functions preserve original numeric text in the returned params map.
That preserved text is not the canonical number spelling used for the digest.

### 3.2 Non-gateway decision records

`HashRecord` is a separate, narrower canonicalization API.
See [`DECISION.md`](../../pkg/receiptspec/DECISION.md).
Its digest can also occupy `params_sha256`.
It accepts a JSON value, not just an object.
Arrays and scalar roots are accepted by the implementation.
It rejects duplicate names, invalid UTF-8, lone surrogates, and depth greater than 32.
Input and canonical output are each limited to `1048576` bytes.

Its number tokens must match `^-?(0|[1-9][0-9]*)$`, excluding `-0`.
Their integer values must fall within `[-9007199254740991, 9007199254740991]`.
Fractions and exponent tokens return `ErrUnsupportedNumber`.
For non-`json.RawMessage` inputs, Go `json.Marshal` runs first.
Thus a Go float containing `1.0` can marshal as `1` and pass.
Other invalid records return `ErrInvalidRecord`.
Unlike gateway params, this API does not reject Unicode noncharacters.
It uses the same UTF-16 sorting and minimal string escaping described above.
`VerifyRecordBinding` recomputes this digest and returns `ErrRecordMismatch` on inequality.
It does not check the receipt signature or chain.

## 4. Signatures and key IDs

Signatures use plain Ed25519 from Go `crypto/ed25519`.
There is no Ed25519ph mode or Ed25519 context.
The signed message is the raw 32-byte `entry_hash`.
It is not the hexadecimal hash or the full preimage.
`Seal` sets `signer_kid`, computes the hash, then signs that hash.
It requires the signer to return exactly 64 signature bytes.

The standard key ID is:

```text
"ed25519:" || lowercase_hex(SHA256(raw_32_byte_public_key)[0:8])
```

The SHA-256 slice contains the first 8 digest bytes, yielding 16 hex characters.
The resulting key ID occupies 24 ASCII bytes.
`internal/signer.ComputeKID` forwards to `receiptspec.ComputeKID`.
Receipt validation treats key IDs as opaque required strings.
Verification looks up their supplied names. It does not recompute key IDs.

The signer store's `HMAC-SHA256(master_key, "agentgate.signer.v1")` derives an at-rest encryption key.
That storage derivation is not a key ID or receipt signature input.
Rotation uses externally supplied public keys with inclusive sequence windows.
The previous key does not sign the new key.
A nil `valid_until_seq` means no upper bound.
You must obtain key history through a trusted channel.

## 5. Chain linkage and producer behavior

Genesis has sequence `1` and a `prev_hash` containing 32 zero bytes.
Each later receipt increments the sequence by exactly 1.
Its `prev_hash` equals the preceding receipt's `entry_hash`.
A partial chain needs a trusted preceding `(seq, entry_hash)` anchor.
The verifier does not sort receipt input.

`NewChain(signer, 0, zero_hash)` starts at genesis.
`Next` advances the in-memory head only after validation and signing succeed.
A zero requested timestamp uses `time.Now().UnixNano()`, converted to `uint64`.
Other timestamps are used unchanged.
Sequence arithmetic uses Go `uint64` arithmetic.
Advancing after `MaxUint64` produces sequence 0, which validation rejects.

The gateway ledger reads its committed head inside `BEGIN IMMEDIATE`.
It allocates sequences, sets timestamps, signs, and inserts before `COMMIT`.
Successful append results require that commit.
Rolled-back transactions consume no sequence.
SQLite stores binary hashes and signatures as raw blobs.
Rows store `format_version = 1`; a nil delegation list is stored as `[]`.
SQLite integer storage cannot represent the entire unsigned wire range.
The boundary vectors exercise the public protocol, not SQLite capacity.

A suffix can disappear without breaking any remaining link.
Without a separately expected endpoint or a manifest, verification cannot detect that deletion.
Even a signed manifest only proves the range selected by its signer.
It does not prove that every external action was recorded.
SSE v1 receipts attest header-time upstream acceptance, not final stream bytes or completion.
The [stream digest proposal](proposals/receipt-stream-digest.md) remains unimplemented.

## 6. Delegation elements

The gateway delegation verifier emits 1 digest per appended Biscuit attenuation block.
The authority block is excluded.
Elements follow block append order.
An authority-only grant produces a nil list.
For each block index `i`, the element is:

```text
lowercase_hex(SHA256(UTF8(biscuit.Code()[i])))
```

`biscuit.Code()` supplies the library's Datalog rendering.
The digest does not cover raw token bytes, signatures, or the original source spelling.
There is no additional domain prefix for these block digests.
The receipt hashes each 64-character hex element as an LP ASCII string, not as 32 decoded bytes.
The receipt verifier does not reconstruct or authorize Biscuit tokens.

The receipt validator's accepted space is wider than the gateway's output.
It accepts empty elements and nonhex ASCII, including controls other than NUL.
It does not require exactly 64 characters or valid hex.
The receipt limit is 32 elements.
The gateway delegation verifier's default limit is 8 attenuation blocks and 4096 token bytes.
Those configurable gateway limits do not change receipt encoding.

## 7. JSONL encoding

`WriteJSONL` emits 1 JSON object per line, followed by LF.
`MarshalJSONLReceipt` returns the object without LF.
It does not validate semantic receipt fields before marshaling.
Whitespace and JSON member order are not hash inputs.

The writer emits receipt members in this order:

| Order | Member | JSON type and encoding |
|---|---|---|
| 1 | `type` | String `"receipt"` |
| 2 | `format_version` | Integer `1` |
| 3 | `seq` | Unsigned integer, unquoted |
| 4 | `timestamp_unix_ns` | Unsigned integer, unquoted |
| 5 | `human_principal` | String |
| 6 | `agent_key_id` | String |
| 7 | `delegation_chain` | String array; nil becomes `null`, empty becomes `[]` |
| 8 | `service` | String |
| 9 | `action` | String |
| 10 | `params_sha256` | String, 64 lowercase hex characters |
| 11 | `policy_decision` | String |
| 12 | `status_code` | Integer, unquoted |
| 13 | `latency_ms` | Integer, unquoted |
| 14 | `error` | String, present even when empty |
| 15 | `prev_hash` | String, 64 lowercase hex characters |
| 16 | `entry_hash` | String, 64 lowercase hex characters |
| 17 | `signer_kid` | String |
| 18 | `signature` | String, 128 lowercase hex characters |

No field uses base64.
Do not parse 64-bit integers through binary64 in another language.
Use exact integer parsing.
JSON strings use Go `encoding/json` escaping, not the canonical params serializer.
The writer escapes `<`, `>`, `&`, U+2028, and U+2029.
Those JSON escape choices do not affect decoded receipt hashes.

The reader trims surrounding whitespace and skips empty lines.
Its scanner has a `1 << 24` maximum token buffer.
Oversized lines or scanner errors fail parsing.
It accepts uppercase or lowercase hex, but rejects invalid hex and incorrect decoded lengths.
It rejects unsupported or missing receipt `format_version`.
Numeric tokens must fit their destination Go integer types.
Semantic checks happen during verification, not parsing.

`encoding/json` parsing accepts unknown members and case-insensitive field matches.
Duplicate members follow Go's merge or replacement behavior. They are not rejected.
Invalid UTF-8 and lone surrogate escapes in JSON strings can become U+FFFD.
Unlike params decoding, JSONL parsing does not explicitly reject them.
Subsequent hashing covers the decoded strings.
These are compatibility facts, not recommendations for new serializers.

Missing or empty `type` defaults to receipt parsing.
`"key"` selects a key line and `"manifest"` selects a manifest line.
All other type strings also take the receipt parser.
Key and manifest parsers do not separately validate `type`.
Keys accumulate in input order.
A later manifest overwrites an earlier manifest.
Manifest and key lines need not precede receipts when parsing.
The export writer emits manifest, keys, then ascending receipts.

### 7.1 Key lines and trust files

Key lines contain these members in writer order:

```text
type, kid, public_key_hex, valid_from_seq, valid_until_seq
```

`type` is `"key"`. `kid` and `public_key_hex` are strings.
The public key is 32 raw bytes encoded as 64 lowercase hex characters.
`valid_from_seq` is an unsigned 64-bit JSON integer.
`valid_until_seq` is an unsigned 64-bit JSON integer or `null`.
Missing `valid_until_seq` also decodes as nil.
Key lines have no `format_version`.
The trust-file shape is `{"keys":[{...}]}`, with the same key members except `type`.
`LoadTrustedKeys` rejects empty key lists and malformed public keys.
It does not reject duplicate IDs or inconsistent validity windows.
For receipt lookup, the last matching trust entry wins.
For manifest lookup, the first matching trust entry wins.
Supply distinct key IDs.

## 8. Export manifests

### 8.1 Keyset digest

Copy embedded keys and sort them by `kid`, using Go string byte ordering.
For each key, append:

```text
LP(kid)
raw_public_key_bytes
U64LE(valid_from_seq)
00                                      # no upper bound
```

For a bounded window, replace that final `00` with:

```text
01 || U64LE(valid_until_seq)
```

SHA-256 the concatenation.
There is no domain prefix or key count.
Public keys have no length prefix.
The empty keyset digest is SHA-256 of empty bytes.
Distinct key IDs make this digest independent of input order.
Equal IDs have no secondary sorting key.

### 8.2 Manifest preimage and signature

Concatenate these fields:

| Order | Input | Encoding |
|---|---|---|
| 1 | Domain | ASCII `agentgate.receipt.export-manifest.v1`, then `00` |
| 2 | `format_version` | `U32LE(uint32(format_version))` |
| 3 | `requested_from` | `U64LE` |
| 4 | `requested_to` | `U64LE`; zero means omitted request bound |
| 5 | `resolved_to` | `U64LE` |
| 6 | `count` | `U64LE(uint64(count))`; Go field type is `int` |
| 7 | `anchor_seq` | `U64LE` |
| 8 | `anchor_hash` | 32 raw bytes |
| 9 | `first_entry_hash` | 32 raw bytes |
| 10 | `last_entry_hash` | 32 raw bytes |
| 11 | `head_seq` | `U64LE` |
| 12 | `head_hash` | 32 raw bytes |
| 13 | `keyset_digest` | 32 raw bytes |
| 14 | `signer_kid` | `LP(signer_kid)` |

`manifest_hash = SHA256(preimage)`.
The signature is plain Ed25519 over those 32 hash bytes.
The signature itself is excluded from the preimage.
`ComputeManifestHash` does not validate manifest semantics.
Direct calls to `VerifyManifest` do not check the version.
JSONL manifest parsing requires `format_version = 1`.
No dedicated manifest string-length, positive-count, or range-consistency checks exist.

Manifest JSON members appear in this order:

```text
type, format_version, requested_from, requested_to, resolved_to, count,
anchor_seq, anchor_hash, first_entry_hash, last_entry_hash, head_seq,
head_hash, keyset_digest, signer_kid, signature
```

`type` is `"manifest"`.
`requested_to` is omitted when zero.
All range and sequence fields are unsigned 64-bit JSON integers.
`format_version` and `count` are Go `int` JSON integers.
`signer_kid` is a string.
All hashes use 64 lowercase hex characters.
The signature uses 128 lowercase hex characters.
The reader also accepts uppercase hex and requires exact decoded lengths.

### 8.3 Gateway export construction

The export endpoint requires `from >= 1`.
If supplied, `to` must be at least `from`.
It reads head, anchor, rows, and signer keys from 1 read-only SQLite snapshot.
An omitted or excessive `to` resolves to the snapshot head.
An explicit lower `to` resolves to that bound.
`from > head + 1` is rejected.
Ranges exceeding 10000 receipts are rejected, not truncated.

`anchor_seq = from - 1`.
The anchor hash comes from that sequence, or zero bytes at genesis.
`first_entry_hash` and `last_entry_hash` come from the selected rows.
For an empty selection, both equal the anchor hash.
`head_seq` and `head_hash` describe the snapshot head.
The keyset includes all stored signer keys.
The active signer signs the manifest after the read transaction finishes.
The endpoint can emit an empty selection, but `VerifyChain` rejects an empty chain.

## 9. Verification algorithm and error classes

### 9.1 Bundle checks

`VerifyBundle` performs these steps:

1. Use explicit trusted keys when nonempty. Otherwise use embedded key lines.
2. Start with a genesis anchor.
3. If a manifest exists, find its `signer_kid` in trusted keys.
4. Reject an unknown manifest signer with `ManifestError` wrapping `ErrUnknownSignerKID`.
5. Reject invalid key length or signature with `ManifestError` wrapping `ErrManifestSignatureInvalid`.
6. Recompute the embedded keyset digest.
7. Reject inequality with `ManifestError` wrapping `ErrKeysetDigestMismatch`.
8. Use the signed manifest's anchor.
9. Unless explicitly overridden, expect `(resolved_to, last_entry_hash)` as the endpoint.
10. Run the chain checks below.

Manifest signing keys are not checked against sequence validity windows.
Explicit trust replaces embedded keys for lookup, but embedded keys still determine the signed keyset digest.
Embedded keys alone prove internal consistency, not the producer's identity.
Pin an independently obtained trust file when identity matters.

The verifier does not separately check manifest `count`, `requested_from`, `requested_to`, or `first_entry_hash`.
It does not compare `head_seq` or `head_hash` against receipts.
Those fields are signed but are not independently enforced.
An explicit expected head replaces the manifest-derived endpoint, rather than adding another endpoint comparison.

### 9.2 Chain checks

`VerifyChain` returns a Go error if no trusted keys or no receipts exist.
These errors are `ErrNoTrustedKeys` and `ErrEmptyChain`, in that order.
Otherwise, process receipts in supplied order:

1. Require `seq == anchor.seq + 1 + index`.
2. Require `prev_hash` to equal the anchor hash or preceding verified entry hash.
3. Look up `signer_kid`. An absent key returns `ErrUnknownSignerKID`.
4. Require the receipt sequence inside that key's inclusive validity window.
5. Validate every semantic field from section 1.
6. Recompute the canonical preimage and SHA-256.
7. Require equality with `entry_hash`.
8. Verify Ed25519 over the computed 32-byte hash.
9. Advance the verified count and head only after all checks succeed.
10. If supplied, require the final sequence and hash to match the expected head.

Except unknown signers, per-receipt failures return `OK = false` without a Go error.
The first failing check determines the result:

| Check | `VerifyResult.Reason` |
|---|---|
| Unexpected sequence | `sequence_gap_or_not_genesis_anchored` |
| Bad previous hash | `prev_hash_mismatch` |
| Key inactive at sequence | `signer_kid_inactive_at_seq` |
| Invalid semantic field | `invalid_receipt_fields` |
| Recomputed hash differs | `entry_hash_mismatch` |
| Bad key length or Ed25519 signature | `signature_invalid` |
| Final endpoint differs | `expected_head_mismatch` |

`FailedAtSeq` is the receipt's sequence, except endpoint mismatch uses the expected sequence.
`VerifiedCount` counts only fully checked receipts.
Unknown signer errors return a zero result, discarding earlier progress.
`Complete` is true only when a supplied or manifest-derived endpoint matches.
It means completeness against that endpoint, not against unseen future receipts.
Without an endpoint, a valid prefix returns `OK = true` and `Complete = false`.
`VerifyReceipt` checks fields, entry hash, and signature only.
It does not check key-ID ownership, validity windows, sequence continuity, or previous-hash linkage.

The CLI exits `0` for success, `1` for mismatches, and `2` for parsing or configuration errors.
All `ManifestError` values exit `1`, including an unknown manifest signer.
An unknown receipt signer exits `2`.
CLI range reporting uses `resolved_to == head_seq` to label an export `full`.
That label does not add a head-hash comparison.

## 10. Worked bytes

The committed generator computes these examples through the real receipt implementation.
Run it from the repository root:

```sh
go run ./pkg/receiptspec/testdata/v1/gen -out pkg/receiptspec/testdata/v1
go test ./pkg/receiptspec -run 'TestV1(GoldenVectors|VersionLock|WorkedExample)' -count=1
```

[`example.json`](../../pkg/receiptspec/testdata/v1/example.json) contains the complete preimage, key, hash, and signature.
The corresponding input is [`valid-genesis.jsonl`](../../pkg/receiptspec/testdata/v1/valid-genesis.jsonl).
Its receipt has sequence 1, timestamp 1, principal `h`, agent `a`, service `s`, and action `x`.
Its params digest covers `{}`.
Its decision is `allow`, status is 100, latency is 0, and error is empty.
Its previous hash is 32 zero bytes.

The deterministic Ed25519 seed is:

```text
000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
```

The public key and derived ID are:

```text
03a107bff3ce10be1d70dd18e74bc09967e4d6309ba50d5f1ddc8664125531b8
ed25519:56475aa75463474c
```

The preimage occupies 183 bytes.
Offsets below are zero-based and inclusive:

| Offsets | Meaning | Hex bytes |
|---|---|---|
| 0..25 | Domain and NUL | `6167656e74676174652e726563656970742e686173682e763100` |
| 26..33 | Sequence 1 | `0100000000000000` |
| 34..41 | Timestamp 1 | `0100000000000000` |
| 42..46 | Principal | `0100000068` |
| 47..51 | Agent ID | `0100000061` |
| 52..55 | Delegation count 0 | `00000000` |
| 56..60 | Service | `0100000073` |
| 61..65 | Action | `0100000078` |
| 66..97 | Params digest | `44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a` |
| 98..106 | Decision | `05000000616c6c6f77` |
| 107..110 | Status 100 | `64000000` |
| 111..118 | Latency 0 | `0000000000000000` |
| 119..122 | Empty error | `00000000` |
| 123..154 | Genesis previous hash | 32 `00` bytes |
| 155..182 | Signer ID | `18000000656432353531393a35363437356161373534363334373463` |

SHA-256 of that exact concatenation is:

```text
b9647a242fa29e0bbb32d5d3e1157165b4a5ebc24a1b04c71d6f4b4d03cba138
```

The Ed25519 signature over those 32 bytes is:

```text
9b0b99df7d812a725ed1f810bbf913bf86b15de3152808cf97dc9ecdc45eb6e2f
0481c80490d77d140cc48152c2d337871ebffa343dc69c5f18ee4d294ea1705
```

The signed `valid-chain` manifest hashes to:

```text
8e8fb7b9943680a29e9ed35e93ad243d59e2b6ddf3c95c003e2c678d99810155
```

Gateway params examples were also computed with `receipt.DigestParams`:

| Input | Canonical UTF-8 |
|---|---|
| `{"b":2e0,"a":1.0}` | `{"a":1,"b":2}` |
| `{"n":-0}` | `{"n":0}` |
| `{"n":9007199254740993}` | `{"n":9007199254740992}` |
| `{"n":1e-400}` | `{"n":0}` |
| `{"n":1e20}` | `{"n":100000000000000000000}` |
| `{"n":1e21}` | `{"n":1e+21}` |
| `{"n":1e-6}` | `{"n":0.000001}` |
| `{"n":1e-7}` | `{"n":1e-7}` |

SHA-256 of `{"a":1,"b":2}` is:

```text
43258cff783fe7036d8a43033f830adfc60ec037382473548ac742b888292777
```

## 11. Golden vectors and version lock

[`vectors.json`](../../pkg/receiptspec/testdata/v1/vectors.json) lists every input file and expected result.
Each vector includes JSONL, external public keys, and embedded keys.
Bounded exports include their manifest as the first JSONL line.
Tests exercise both explicit and embedded trust.

| Vector | Expected class |
|---|---|
| `valid-genesis` | `ok`, no completeness claim |
| `valid-chain` | `ok`, 3 receipts, rotation, delegation digests, Unicode, maximum timestamp and latency |
| `valid-partial` | `ok`, signed non-genesis anchor |
| `valid-boundary` | `ok`, maximum sequence and string sizes, 32 delegation elements |
| `valid-unpinned-prefix` | `ok`, deleted suffix cannot be detected without an endpoint |
| `valid-unchecked-manifest-count` | `ok`, signed count is not compared with receipts |
| `tampered-body` | `entry_hash_mismatch` |
| `reordered-chain` | `sequence_gap_or_not_genesis_anchored` |
| `missing-middle` | `sequence_gap_or_not_genesis_anchored` |
| `missing-tail-with-manifest` | `expected_head_mismatch` |
| `wrong-key-signature` | `signature_invalid` |
| `bad-prev-hash` | `prev_hash_mismatch` |
| `invalid-fields` | `invalid_receipt_fields` |
| `inactive-signer` | `signer_kid_inactive_at_seq` |
| `unknown-signer` | `ErrUnknownSignerKID` |
| `empty-chain` | `ErrEmptyChain` |
| `manifest-mismatch` | `ManifestError/ErrManifestSignatureInvalid` |
| `manifest-keyset-mismatch` | `ManifestError/ErrKeysetDigestMismatch` |
| `manifest-unknown-signer` | `ManifestError/ErrUnknownSignerKID` |
| `manifest-endpoint-mismatch` | `expected_head_mismatch` |

Seeds are public, fixed, test-only values. Never deploy these keys.
Seed A contains bytes `00` through `1f`. Seed B contains `20` through `3f`.
The generator writes deterministic files and does not update `encoding-lock.json`.
That separate file pins SHA-256 digests of canonical receipt and manifest preimages for every accepted vector.
`TestV1VersionLock` compares real encoding against those committed digests.
Do not refresh the v1 lock to hide an encoding change.
The older independent binary fixture remains in `internal/receipt/testdata/v1`.
Its filename includes `genesis`, but its encoded sequence is `MaxUint64`, not 1.

## 12. Versioning

Receipt JSONL and SQLite rows declare `format_version = 1`.
Manifest JSONL independently declares `format_version = 1`.
Readers reject other versions with `ErrUnsupportedFormatVersion`.
Missing JSON versions decode to 0 and are rejected.
The receipt binary preimage selects v1 through `agentgate.receipt.hash.v1`.
The manifest preimage includes both its v1 domain and its integer version.

Keep v1 byte encodings and accepted semantics frozen.
Changes to field order, widths, endianness, prefixes, or domain strings require v2.
Changing signed fields, hash algorithms, signature algorithms, or signature messages requires v2.
Changing params canonicalization or delegation digest meaning requires v2.
Adding policy decisions, changing bounds, or making previously accepted receipts invalid requires v2.
New authenticated payload fields also require v2.
Unsigned JSON metadata can be ignored today, but cannot silently gain signed meaning.
Changing writer formatting alone need not change receipt hashes.
Do not claim that newly emitted values are interoperable unless old v1 readers accept them.
A v2 implementation needs a new format version, separate domain strings, and `RECEIPT-v2.md`.
Preserve v1 vectors and verification support.

## 13. Existing documentation differences

Code takes precedence for this freeze.
No discrepancy below changes production behavior.

- `DECISION.md` calls decision records objects. `CanonicalRecord` also accepts arrays and scalar roots.
- `DECISION.md` names a canonical-size limit. The code also limits raw input to 1 MiB.
- The old `genesis-unicode-max.bin` fixture contains sequence `MaxUint64`, not a genesis chain.
- Manifest comments describe bound metadata. Verification enforces the anchor, endpoint, signature, and keyset digest only.
- The export endpoint can emit an empty range. The verifier rejects it with `ErrEmptyChain`.
- Gateway delegation output is 64-character hex. Receipt validation accepts shorter nonhex ASCII elements.

The stream-digest proposal correctly describes an unimplemented v2 feature.
Gateway params and decision records intentionally expose different numeric acceptance rules.
They must not be treated as interchangeable validators.
