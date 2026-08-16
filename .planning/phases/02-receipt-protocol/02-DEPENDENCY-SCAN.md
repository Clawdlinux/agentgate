---
dependency: github.com/gowebpki/jcs
tag: v1.0.1
commit: 1a4242a66e1a8e03d7458324d0bc95c327527cbb
scanner: SkillSpector 2.8.2
scanned: 2026-08-13
llm_analysis: false
score: 3
findings: 0
recommendation: SAFE
status: approved
---

# Phase 2 Dependency Scan

SkillSpector scanned the exact pinned source before any package installation.

## Historical Blocked Verdict

`DO_NOT_INSTALL`

This verdict applies only to `github.com/go-json-experiment/json` at commit
`4849db3c2f7e2cc8a9816ebf68aafb0a046dec5b`. No `go get` command ran for it.

## Blocking Finding

| ID | Severity | File | Finding |
|----|----------|------|---------|
| TM1 | HIGH | `migrate.sh:7` | The script runs `rm -r $JSONROOT/*.go $JSONROOT/internal $JSONROOT/jsontext $JSONROOT/v1`. |

This script can recursively delete source files and directories. The static scanner classifies it as tool parameter abuse.

## Other Findings

- 4 medium findings in `bench_test.go` classify repeated benchmark data as context stuffing.
- 2 low findings classify standard BSD license text as scope creep.

Those appear unrelated to runtime package behavior. They do not override the blocking verdict.

## Recovery Options

1. Choose a different RFC 8785 implementation and scan its pinned source.
2. Vendor a narrowly reviewed canonicalization implementation only after a new approved plan.
3. Change the dependency policy through an explicit user decision outside auto mode.

Phase 2 remained blocked until the recovery below was verified.

## SAFE Recovery

The selected replacement is approved for Phase 2.

| Field | Verified value |
|-------|----------------|
| Module | `github.com/gowebpki/jcs` |
| Tag | `v1.0.1` |
| Commit | `1a4242a66e1a8e03d7458324d0bc95c327527cbb` |
| SkillSpector | `2.8.2`, static scan, LLM disabled |
| Verdict | `SAFE` |
| Score | `3` |
| Findings | `0` |
| License | `Apache-2.0` |
| Own tests | Candidate test suite passes |
| Module Sum | `h1:Qjzg8EOkrOTuWP7DqQ1FbYtcpEbeTzUoTN9bptp8FOU=` |
| GoModSum | `h1:CID1cNZ+sHp1CCpAR8mPf6QRtagFBgPJE0FCUQ6+BrI=` |

The active approval applies only to this module, tag, and commit. The earlier
`DO_NOT_INSTALL` result remains historical and still blocks that package.