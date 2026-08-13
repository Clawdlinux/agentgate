---
dependency: github.com/go-json-experiment/json
commit: 4849db3c2f7e2cc8a9816ebf68aafb0a046dec5b
scanner: SkillSpector 2.8.2
scanned: 2026-08-13
llm_analysis: false
score: 52
severity: HIGH
recommendation: DO_NOT_INSTALL
status: blocked
---

# Phase 2 Dependency Scan

SkillSpector scanned the exact pinned source before any package installation.

## Verdict

`DO_NOT_INSTALL`

The plan and global install policy require a hard stop. No `go get` command ran.

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

Phase 2 remains blocked until one recovery path is planned and verified.