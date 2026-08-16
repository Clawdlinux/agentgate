# Phase 2: Receipt Protocol - Context

**Gathered:** 2026-08-13
**Status:** Ready for planning

<domain>
## Phase Boundary

Define and implement the deterministic, privacy-limited receipt protocol primitives in `internal/receipt`. Phase 2 owns the semantic receipt type, strict validation, parameter digest, canonical entry-hash input, entry-hash computation, independent encoder tests, and immutable fixtures. It does not sign, persist, export, or wire receipts into `/v1/act`.

</domain>

<requirements>
## Requirement Mapping

- **RCPT-01:** D-01 through D-11 define fixed fields, order, types, limits, and versioned domain separation.
- **RCPT-02:** D-12 through D-16 define semantic parameter equivalence and SHA-256 digest behavior.
- **RCPT-03:** D-16, D-18, and D-19 prohibit raw parameters, credentials, bodies, and provider text.
- **RCPT-04:** D-20 and D-22 require production and independent reference encoders to agree.
- **RCPT-05:** D-23 and D-24 define immutable golden fixtures and boundary coverage.

</requirements>

<decisions>
## Implementation Decisions

### Hash Boundary And Domain Separation
- **D-01:** Canonical bytes mean the entry-hash preimage. They are not a persistence format, JSONL format, or complete binary receipt record.
- **D-02:** Prefix every hash input with the exact ASCII bytes `agentgate.receipt.hash.v1` followed by one NUL byte. A protocol change requires a new domain version.
- **D-03:** Encode fields after the domain in this exact order: `seq`, `timestamp_unix_ns`, `human_principal`, `agent_key_id`, `delegation_chain`, `service`, `action`, `params_sha256`, `policy_decision`, `status_code`, `latency_ms`, `error`, `prev_hash`, `signer_kid`.
- **D-04:** Exclude `entry_hash` and `signature` from the hash input because they are derived. Include `signer_kid` so changing key identity changes `entry_hash`.
- **D-05:** `entry_hash` is SHA-256 over the complete canonical hash input. Phase 3 signs this 32-byte hash with Ed25519.

### Field Types, Encoding, And Bounds
- **D-06:** Keep the PRD Go field types. Encode `uint64` values as 8-byte little-endian. Encode `status_code` as signed 4-byte little-endian after range validation. Encode `latency_ms` as signed 8-byte little-endian.
- **D-07:** Encode strings as uint32 little-endian byte length followed by exact UTF-8 bytes. Require valid UTF-8, reject NUL, and perform no Unicode normalization.
- **D-08:** Encode `delegation_chain` as uint32 little-endian element count followed by ordered length-prefixed elements. R2 produces an empty list but the encoder supports the future field.
- **D-09:** Require `seq >= 1`, `timestamp_unix_ns >= 1`, `100 <= status_code <= 599`, and `latency_ms >= 0`. Never clamp or coerce invalid values.
- **D-10:** Require nonempty `human_principal`, `agent_key_id`, `service`, `action`, `policy_decision`, and `signer_kid`. Byte limits are 256, 128, 64, 128, 16, and 128 respectively.
- **D-11:** Limit `delegation_chain` to 32 ordered elements and each element to 64 ASCII bytes. Limit `error` to 64 ASCII bytes.

### Parameter Digest Semantics
- **D-12:** Canonicalize parameters using RFC 8785 JSON Canonicalization Scheme. Use a maintained standards implementation. Do not hand-roll JSON number or string canonicalization.
- **D-13:** `DigestParams` accepts raw JSON bytes so duplicate object names can be rejected before data is collapsed into a Go map. Phase 4 may adapt request decoding later.
- **D-14:** The top-level parameter value must be an object. Omitted bytes, whitespace-only bytes, or JSON `null` normalize to the empty object `{}`.
- **D-15:** Reject duplicate names, invalid Unicode, non-I-JSON numbers, nesting beyond 32 levels, and canonical output above 1 MiB. Do not coerce invalid input.
- **D-16:** Hash the RFC 8785 bytes with SHA-256 and return only `[32]byte`. `Receipt` never stores raw or canonical parameter JSON.

### Privacy And Closed Vocabularies
- **D-17:** `policy_decision` accepts exactly `allow`, `deny`, or `rate_limited`. Encode the selected ASCII string as length-prefixed bytes.
- **D-18:** `error` is empty or matches `^[a-z0-9_]{1,64}$`. It is a stable machine code only. Never include provider messages, response bodies, tokens, or request content.
- **D-19:** Receipt fields may contain identifiers required by the PRD, but no field may contain OAuth tokens, raw parameters, upstream bodies, or unrestricted provider errors.

### Package API And Fixtures
- **D-20:** Expose a small pure API centered on `Receipt`, `Validate`, `DigestParams`, `CanonicalHashInput`, and `ComputeEntryHash`. Return errors for invalid input. Do not mutate caller slices.
- **D-21:** Keep signing, key generation, key rotation, database interfaces, request middleware, and export formats out of `internal/receipt` during R2.
- **D-22:** Prove encoder independence with an external-package test reference encoder. It may use the public `Receipt` fields and protocol constants but cannot call production encoding helpers.
- **D-23:** Check in a human-readable fixture manifest plus immutable binary golden files and documented SHA-256 values. Cover genesis zero `prev_hash`, exact Unicode bytes, empty delegation, maximum integer values, and validation boundaries.
- **D-24:** Golden updates require an explicit protocol-version change and reviewed fixture replacement. Tests must never rewrite goldens automatically.

### Claude's Discretion
- Exact internal file split within `internal/receipt`.
- Choice of maintained RFC 8785 package after research verifies API, license, and duplicate-name behavior.
- Error type names and test helper names.
- Exact fixture case names and manifest schema.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Product Contract
- `PRD-receipts-oss.md` — Defines R2 fields, privacy boundary, task order, and acceptance criteria.
- `.planning/PROJECT.md` — Defines the product boundary and receipt claims.
- `.planning/REQUIREMENTS.md` — Defines RCPT-01 through RCPT-05.
- `.planning/ROADMAP.md` — Defines the Phase 2 goal and success criteria.

### Existing And Reference Code
- `internal/gateway/gateway.go` — Shows current `map[string]interface{}` parameter decoding that Phase 4 may later adapt.
- `internal/audit/logger.go` — Shows existing audit names but is not a protocol implementation to extend.
- `../agentic-operator-core/pkg/audit/chain.go` — Defines the reusable little-endian and uint32 length-prefix pattern.
- `../agentic-operator-core/pkg/audit/audit_test.go` — Shows chain, tamper, and key-ID test patterns. HMAC behavior is not reusable.

### External Standard
- RFC 8785, JSON Canonicalization Scheme — Defines parameter canonicalization and I-JSON number/string behavior.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `../agentic-operator-core/pkg/audit/chain.go`: Reuse little-endian fixed integers, uint32 length prefixes, and SHA-256 preimage construction.
- `../agentic-operator-core/pkg/audit/audit_test.go`: Reuse external-package tests and deterministic chain assertions as test-shape references.
- `internal/gateway/gateway_test.go`: Reuse table-driven conventions for parameter objects and Unicode cases.

### Established Patterns
- Internal packages use constructors or pure functions with package-prefixed errors.
- Tests are table-driven and use the standard Go test package.
- The current gateway decodes parameters before dispatch. R2 must stay pure and avoid changing that path.
- The existing async audit logger stores raw error strings and can drop entries. It must not control the new protocol.

### Integration Points
- New `internal/receipt` package owns the semantic protocol contract.
- Phase 3 consumes `ComputeEntryHash` and adds Ed25519 signing.
- Phase 4 constructs receipts from authenticated request outcomes and persists them synchronously.
- Phase 5 consumes the same canonical hash input during offline verification.

</code_context>

<specifics>
## Specific Ideas

- Keep canonical hash bytes independent of JSON persistence and export formats.
- Bind `signer_kid` into `entry_hash` before Phase 3 signs it.
- Preserve exact UTF-8 bytes so independent implementations do not need a hidden normalization policy.
- Use stable error codes to keep auditor artifacts shareable.

</specifics>

<deferred>
## Deferred Ideas

- Ed25519 signing and key lifecycle belong to Phase 3.
- SQLite storage, sequencing, and request-path integration belong to Phase 4.
- JSONL and SQLite offline verification belong to Phase 5.
- Biscuit delegation validation and lineage production belong to Phase 8.

</deferred>

---

*Phase: 02-receipt-protocol*
*Context gathered: 2026-08-13*