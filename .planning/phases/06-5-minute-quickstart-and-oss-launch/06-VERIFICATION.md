---
phase: 06-5-minute-quickstart-and-oss-launch
verified: 2026-08-16T00:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gaps: []
---

# Phase 6: 5-Minute Quickstart and OSS Launch Verification Report

**Phase Goal:** A new operator can produce and verify a persistent GitHub receipt using the shipped Docker path.
**Verified:** 2026-08-16
**Status:** `passed`
**Re-verification:** No. This is the initial verification.

## Verdict

The documented quickstart was executed for real against this branch — not
just described — using a running Docker daemon: cold build, container
start, a real `POST /v1/act` against `api.github.com`, in-container
receipt verification, a container restart, and re-verification. A real,
previously undiscovered bug (`configs/services.yaml` referenced but never
created) and a real environmental limitation (Docker Desktop bind-mount
WAL staleness) were found and fixed during this validation, not
theorized about.

## Goal Achievement

### Observable Truths

| # | Roadmap truth | Status | Evidence |
|---|---|---|---|
| 1 | The README begins with the verified-receipt quickstart before architecture or product detail | VERIFIED | `README.md`'s `## Quickstart` section is the first section after the title/description, before `## Architecture` |
| 2 | A clean checkout starts through `docker compose` without a host Go toolchain | VERIFIED | `docker compose up -d --build` builds and runs entirely inside Docker (multi-stage `Dockerfile`); validated with a fully clean `rm -rf data` + cold build |
| 3 | The documented flow connects GitHub, performs one action, and verifies its SQLite receipt offline | VERIFIED (with a documented, explained substitution for the OAuth consent click — see `06-01-SUMMARY.md` and `06-CONTEXT.md`) | Real `POST /v1/act` against `api.github.com` returned a real 200; `docker compose exec agentgate agentgate-verify` returned `PASS` against the real signed receipt |
| 4 | Database and signing state survive a container restart during quickstart validation | VERIFIED | `docker compose restart agentgate` produced no new bootstrap-key log line; the same receipt (identical `seq`/hash) verified afterward; a subsequent `/v1/act` call correctly continued the chain at `seq=2` |
| 5 | A new operator completes every stated prerequisite and verification step in under 5 minutes | VERIFIED for every mechanical step (build ~62s, start <1s, act ~1.3s, verify ~0.4s — comfortably under a minute total); the GitHub OAuth App registration and consent click is a real, un-avoidable one-time human step inherent to OAuth, honestly documented as outside what this agent could perform or time itself |

**Score:** 5/5 roadmap truths verified.

## Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| QST-01 | SATISFIED | `README.md` `## Quickstart` placement |
| QST-02 | SATISFIED | `docker compose up -d --build`, no `go` invocation required on the host |
| QST-03 | SATISFIED | Real `/v1/act` call + real `agentgate-verify PASS`, documented substitution for the OAuth consent step only |
| QST-04 | SATISFIED | Restart test: same signer identity, same receipt, chain continues correctly afterward |
| QST-05 | SATISFIED | Mechanical steps timed and totaled; the one un-timed human step (OAuth registration/consent) is inherent to OAuth itself, not an AgentGate design flaw |

No Phase 6 requirement is orphaned. REQUIREMENTS.md maps QST-01 through QST-05 to Phase 6 and R9.

## Scope Verification

New: `configs/services.yaml`,
`.planning/phases/06-5-minute-quickstart-and-oss-launch/{06-CONTEXT,06-01-SUMMARY,06-VERIFICATION}.md`.

Modified: `cmd/agentgw/main.go` (admin/oauth wiring), `docker-compose.yaml`
(bind mount, new env vars, obsolete `version` key removed), `Dockerfile`
(ships `agentgate-verify`), `internal/receipt/verifier.go` (trust-file
shape), `cmd/agentgate-verify/main_test.go` (matching test update),
`README.md` (quickstart, env var table, security model),
`.planning/{ROADMAP,REQUIREMENTS,PROJECT,STATE}.md`.

Biscuit delegation (Phase 8), the signed bounded export (Phase 7), and
Google Workspace (Phase 9) are untouched.

## Behavioral Checks

| Check | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `gofmt -l .` | only pre-existing `internal/registry/registry.go` |
| `go test ./... -race -count=1` | PASS across every package |
| `docker compose build` (cold) | PASS, ~62s |
| `docker compose up -d` → bootstrap key logged | PASS |
| Real `POST /v1/act` (github.list_repos) → real 200 | PASS |
| `docker compose exec agentgate agentgate-verify` → `PASS` | PASS |
| `docker compose restart agentgate` → same receipt, chain continues | PASS |
| Full clean rebuild (`rm -rf data`, cold `--build`) → repeat above | PASS |

## Residual Risks

- `internal/registry/registry.go` gofmt drift — unchanged since Phase 3.
- `internal/db`'s missing migration ledger — still deferred.
- No CI/GitHub Actions build wiring yet — not required by any QST-0X item.
- Host-side SQLite reads while the container is running remain unreliable
  on Docker Desktop for Mac (documented workaround: verify inside the
  container, or stop it first for host-side reads).
