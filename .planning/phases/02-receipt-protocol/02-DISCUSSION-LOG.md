# Phase 2: Receipt Protocol - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md. This log preserves the alternatives considered.

**Date:** 2026-08-13
**Phase:** 02-receipt-protocol
**Mode:** auto
**Areas discussed:** Hash boundary and domain separation, field encoding and limits, parameter digest semantics, privacy and fixtures

---

## Hash Boundary And Domain Separation

| Decision | Alternatives | Auto-selected |
|----------|--------------|---------------|
| Canonical bytes are the hash preimage | Complete binary record, JSON wire format | Recommended |
| Exact versioned domain prefix | Version field only, no domain | Recommended |
| Exclude hash and signature, include signer KID | Exclude signer, include all fields | Recommended |
| Protocol primitives only in R2 | SQLite or JSONL in R2 | Recommended |

## Field Encoding And Limits

| Decision | Alternatives | Auto-selected |
|----------|--------------|---------------|
| Little-endian integers and uint32 LP strings | Varints, canonical JSON | Recommended |
| Count plus ordered delegation elements | Joined string, unordered set | Recommended |
| Strict operational ranges | Full machine ranges, clamping | Recommended |
| Preserve exact valid UTF-8 bytes | NFC normalization, ASCII only | Recommended |

## Parameter Digest Semantics

| Decision | Alternatives | Auto-selected |
|----------|--------------|---------------|
| RFC 8785 JCS | `encoding/json`, custom sorted walker | Recommended |
| Omitted and null params become `{}` | Hash null, hash empty bytes | Recommended |
| Reject malformed and oversized inputs | Coerce, accept any JSON | Recommended |
| Store digest only | Store canonical or raw JSON | Recommended |

## Privacy, API, And Fixtures

| Decision | Alternatives | Auto-selected |
|----------|--------------|---------------|
| Stable error code only | Redacted or raw messages | Recommended |
| Locked policy strings | Numeric enum, open string | Recommended |
| Independent external test encoder | Round trip, shared helpers | Recommended |
| Manifest plus binary goldens | Go literals, generated snapshots | Recommended |

## Claude's Discretion

- Internal file split and error type names.
- Maintained RFC 8785 package choice after research.
- Fixture names and manifest schema.

## Deferred Ideas

- Signing and key lifecycle in Phase 3.
- Persistence and request-path wiring in Phase 4.
- Offline verifier sources in Phase 5.
- Biscuit lineage production in Phase 8.