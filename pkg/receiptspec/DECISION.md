# Decision records from non-gateway producers

See the frozen [receipt v1 specification](../../docs/spec/RECEIPT-v1.md) for bytes, signatures, and verification limits.

A producer that is not the AgentGate gateway (for example the Clawdlinux
operator) can record an arbitrary decision as a normal v1 receipt. The wire
format does not change. Any existing `agentgate-verify` checks the chain.

## What goes in the signed receipt

| Field | Value |
|---|---|
| `Service` | The producer, for example `operator`. Max 64 bytes. |
| `Action` | `decision:<action>`, for example `decision:scale`. Max 128 bytes. |
| `PolicyDecision` | One of the existing values: `allow`, `deny`, `rate_limited`. |
| `ParamsSHA256` | `HashRecord(record)`, the SHA-256 of the canonical decision record. |
| `StatusCode` | Any HTTP-style code 100 to 599 that fits, for example 200 or 202. |

Build receipts with `receiptspec.NewChain(signer, prevSeq, prevHash).Next(fields)`.
Store each receipt durably before acting on it.

## The decision record

The record is a JSON object stored beside the receipt (same row, same bucket,
same JSONL bundle). The receipt only carries its hash. A verifier holding the
record calls `VerifyRecordBinding(receipt, recordJSON)` after the chain verifies.

Keep the full outcome in the record. `PolicyDecision` is a coarse, fixed enum
that older verifiers enforce. Adding values to it would make those verifiers
reject new receipts, so it stays as is. Map outcomes like this:

| Operator outcome | `PolicyDecision` | Record `outcome` |
|---|---|---|
| allow | `allow` | `allow` |
| approved | `allow` | `approved` |
| edited | `allow` | `edited` |
| deny | `deny` | `deny` |
| rejected | `deny` | `rejected` |
| require_approval | `deny` | `require_approval` |

Rule: `allow` means the action went ahead. `deny` means it did not, yet.
An approval flow writes two receipts: `require_approval` (deny), then
`approved`, `edited`, or `rejected`. Link them with a field in the record,
for example `"approval_of_seq": 41`.

Example record:

```json
{"record_version":1,"outcome":"edited","approval_of_seq":41,"confidence":"0.875","replicas":3}
```

## Canonical JSON

`CanonicalRecord` and `HashRecord` follow RFC 8785 (JCS) for the subset they
accept:

- Object keys sorted by UTF-16 code units. No duplicate keys.
- No insignificant whitespace.
- Strings as literal UTF-8. Only `"`, `\`, and control characters are escaped.
  Lone surrogate escapes and invalid UTF-8 are rejected.
- Numbers must be integers in `[-(2^53-1), 2^53-1]`. Floats, exponents, and
  `-0` are rejected with `ErrUnsupportedNumber`.
- Max nesting depth 32. Max canonical size 1 MiB.

Encode fractions as decimal strings (`"0.875"`) or integer micro-units
(`875000`). Never as floats. Go `float64` fields that hold whole numbers pass;
fractional ones fail, so do not rely on that.

## Advisor producer and JSONL bundles

The gateway's action receipt always hashes its original params.
The separate advisor producer uses `Service="advisor"` and `Action="decision:advise"`.
Its `AgentKeyID` is the reserved `agentgate:advisor`.
`IsAdvisorReceipt` requires all 3 fields.
Older receipts with matching names and ordinary agent IDs remain ordinary receipts.
Modern API key IDs contain 32 lowercase hex characters, so they cannot equal this reserved identity.
Authentication also rejects the reserved ID in legacy rows and cached identities.
Requests targeting service `advisor` or actions beginning `decision:` return 400 without a receipt.
Registry loading rejects the reserved service name.
Its companion receipt follows the action receipt in the same transaction.
The decision record includes `action_receipt`, with the preceding sequence, entry hash, and params digest.
`VerifyAdvisorRecord` checks that link after `VerifyRecordBinding`.

The default JSONL export remains the frozen v1 format.
It omits separate records and remains readable by released v0.1.x chain verifiers.
Request `GET /v1/receipts/export?from=1&records=1` for the opt-in record extension.
It adds `{"type":"record","seq":42,"record":{...}}` after the corresponding receipt.
This line type is outside the [frozen default format](../../docs/spec/RECEIPT-v1.md#7-jsonl-encoding).
Receipt lines and manifest encoding stay unchanged.
`Bundle.Records` holds these separate objects.
`VerifyBundle` rejects missing advisor records, altered bindings, and orphaned records.
Use the opt-in export or SQLite source for updated advisor evidence verification.
The default export provides the original chain-verification format, not complete advisor evidence.
Do not send the extension to older readers.

A record-enabled range can begin with a companion.
After manifest verification, `VerifyAdvisorRecordAtAnchor` checks its `prev_hash` against the signed anchor hash.
The record's action sequence and entry hash must match the signed anchor sequence and hash.
The record still must match the companion's params digest and policy outcome.
The anchor does not expose the preceding action's params digest.
Include the action receipt to compare that digest independently.
A leading companion without a signed manifest fails verification.
V1 cannot prove that every risky gateway action has an advisor companion.

Go structs, maps, and `json.RawMessage` that carry the same content hash the
same. A test checks the output against the JCS library the gateway uses for
params.
