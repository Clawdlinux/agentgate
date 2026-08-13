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
| 02-01-01 | 01 | 1 | RCPT-02 | Supply-chain gate | Dependency scan and pin follow the fixed SkillSpector disposition policy. | static scan and module pin | `test "$(go list -m -f '{{.Version}}' github.com/gowebpki/jcs)" = "v1.0.1"` | Existing tooling | pending |
| 02-01-02 | 01 | 1 | RCPT-01 | T-02 | Receipt validation and snapshot semantics enforce exact fields, limits, and vocabularies. | table and reflection | `go test ./internal/receipt -run 'Test(ReceiptFieldContract|Validate|Snapshot)' -count=1` | No, Wave 0 | pending |
| 02-01-03 | 01 | 1 | RCPT-02, RCPT-03 | T-01, T-03, T-05 | Strict parameter digest and privacy checks reject ambiguity without leaking sensitive input. | unit, property, and sentinel leakage | `go test ./internal/receipt -run 'Test(DigestParams|Privacy)' -count=1` | No, Wave 0 | pending |
| 02-02-01 | 02 | 2 | RCPT-01 | T-02 | Binary preimage and entry hash fix domain, order, widths, lengths, and exclusions. | binary contract | `go test ./internal/receipt -run 'Test(CanonicalHashInput|ComputeEntryHash)' -count=1` | No, Wave 0 | pending |
| 02-02-02 | 02 | 2 | RCPT-04 | T-01, T-02, T-05 | Independent encoder and fuzz properties prove agreement, purity, and boundary handling. | external package and fuzz | `go test ./internal/receipt -run 'TestReferenceEncoderAgreement' -count=1` | No, Wave 0 | pending |
| 02-02-03 | 02 | 2 | RCPT-05 | T-04 | Immutable fixtures and generator checks prove fixed hashes, read-only tests, and no overwrite. | golden contract and generator probe | `go test ./internal/receipt -run 'TestGoldenFixtures' -count=1` | No, Wave 0 | pending |

---

## Dependency Supply-Chain Gate

Before adding `github.com/gowebpki/jcs`:

1. Verify tag `v1.0.1` resolves to commit `1a4242a66e1a8e03d7458324d0bc95c327527cbb`.
2. Verify committed `02-DEPENDENCY-SCAN.md` records SkillSpector 2.8.2 `SAFE`, score 3, and 0 findings.
3. Verify the pinned source license is Apache-2.0 and its own tests pass.
4. Verify module Sum `h1:Qjzg8EOkrOTuWP7DqQ1FbYtcpEbeTzUoTN9bptp8FOU=`.
5. Verify GoModSum `h1:CID1cNZ+sHp1CCpAR8mPf6QRtagFBgPJE0FCUQ6+BrI=`.
6. Run `go get github.com/gowebpki/jcs@v1.0.1` only after those checks pass.

The approved scan runs before `go get`. Never execute package installer code first.

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

**Approval:** dependency approved; implementation pending