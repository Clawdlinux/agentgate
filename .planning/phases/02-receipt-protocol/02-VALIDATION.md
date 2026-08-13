---
phase: 2
slug: receipt-protocol
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-08-13
---

# Phase 2 - Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing, black-box tests, fuzzing, race detector, and static analysis |
| **Config file** | None |
| **Quick run command** | `go test ./internal/receipt -count=1` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | Under 60 seconds without fuzzing |

---

## Sampling Rate

- **After every task commit:** Run that task's focused receipt tests.
- **After every plan wave:** Run `go test -race ./internal/receipt -count=1`.
- **Before `/gsd:verify-work`:** Run full tests, vet, both 10-second fuzzers, and fixture checks.
- **Max feedback latency:** 60 seconds for deterministic checks.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 02-01-01 | 01 | 1 | RCPT-01 | T-02 | Receipt fields, limits, vocabularies, and snapshot semantics are exact. | table and reflection | `go test ./internal/receipt -run 'Test(ReceiptFieldContract|Validate|Snapshot)' -count=1` | No, Wave 0 | pending |
| 02-01-02 | 01 | 1 | RCPT-02 | One strict parser and RFC 8785 path reject ambiguous JSON and produce stable digests. | unit and property | `go test ./internal/receipt -run 'TestDigestParams' -count=1` | No, Wave 0 | pending |
| 02-01-02 | 01 | 1 | RCPT-03 | Raw parameters and dependency errors never appear in receipt state or public errors. | sentinel leakage | `go test ./internal/receipt -run 'TestPrivacy' -count=1` | No, Wave 0 | pending |
| 02-02-01 | 02 | 2 | RCPT-01 | Domain, field order, widths, lengths, and exclusions define one preimage. | binary contract | `go test ./internal/receipt -run 'Test(CanonicalHashInput|ComputeEntryHash)' -count=1` | No, Wave 0 | pending |
| 02-02-02 | 02 | 2 | RCPT-04 | A black-box reference encoder shares no production encoding helpers. | external package | `go test ./internal/receipt -run 'TestReferenceEncoderAgreement' -count=1` | No, Wave 0 | pending |
| 02-02-02 | 02 | 2 | RCPT-05 | Immutable binary goldens and manifest hashes remain fixed. | golden contract | `go test ./internal/receipt -run 'TestGoldenFixtures' -count=1` | No, Wave 0 | pending |

---

## Dependency Supply-Chain Gate

Before adding `github.com/go-json-experiment/json`:

1. Pin commit `4849db3c2f7e2cc8a9816ebf68aafb0a046dec5b`.
2. Scan the pinned source with `skillspector scan <source> --no-llm --format json`.
3. Proceed only on `SAFE`.
4. Stop for explicit approval on `CAUTION`.
5. Block on `DO_NOT_INSTALL`, scan errors, incomplete analysis, or uninspected files.
6. Verify BSD-3-Clause licensing and expected module checksum.

The scan runs before `go get`. Never execute package installer code first.

---

## Wave 0 Requirements

- [ ] `internal/receipt/receipt_test.go` for fields, bounds, vocabularies, and snapshots.
- [ ] `internal/receipt/params_test.go` for strict JCS behavior and privacy.
- [ ] `internal/receipt/encoding_test.go` for production bytes and hashes.
- [ ] `internal/receipt/receipt_external_test.go` for the independent reference encoder.
- [ ] `internal/receipt/fuzz_test.go` for parser and binary properties.
- [ ] `internal/receipt/testdata/v1/manifest.json` for fixture metadata.
- [ ] `internal/receipt/testdata/v1/genesis-unicode-max.bin` for immutable preimage bytes.

The existing Go test framework needs no installation.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Pinned parser dependency acceptance | RCPT-02 | SkillSpector CAUTION requires human risk acceptance. | Review scan findings, immutable pin, BSD-3-Clause license, and module checksum. Approve only if findings are acceptable. |
| Golden fixture change review | RCPT-05 | Protocol-byte changes require intentional version review. | Confirm tests cannot rewrite fixtures. Compare binary diff and documented SHA-256 before accepting replacement. |

---

## Phase Gate

```bash
go test ./...
go vet ./...
go test -race ./internal/receipt -count=1
go test ./internal/receipt -run '^$' -fuzz '^FuzzDigestParams$' -fuzztime=10s
go test ./internal/receipt -run '^$' -fuzz '^FuzzCanonicalHashInput$' -fuzztime=10s
git diff --check
```

---

## Validation Sign-Off

- [x] All planned task slices have an automated falsifying check.
- [x] Sampling continuity has no 3 consecutive tasks without verification.
- [x] Wave 0 covers every missing test and fixture artifact.
- [x] Commands use no watch mode.
- [x] Deterministic feedback latency is under 60 seconds.
- [x] `nyquist_compliant: true` is set in frontmatter.

**Approval:** pending dependency scan and implementation