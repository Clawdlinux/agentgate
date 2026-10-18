---
phase: 11-contributor-entry-path
plan: 11-01
completed: 2026-08-16
---

# Phase 11 Plan 01: Contributor Entry Path — Summary

## What Was Built

- **`CONTRIBUTING.md`** (new): prerequisites (Go 1.25.0, CGO enabled for
  the SQLite driver, no secrets or database needed to build/test), exact
  `make build`/`make build-verify`/`make test`/`make lint` targets plus the
  raw `go build`/`go vet`/`gofmt -l .`/`go test -race` commands a reviewer
  will actually run, focused-PR guidance (one logical change per PR,
  branch off `main`, clear verification in the description), and a DCO
  sign-off section explicitly distinguishing `git commit -s` (adds a
  `Signed-off-by:` line, asserts the right to submit under the project's
  license) from a cryptographic signature (`git commit -S`, GPG/SSH-based)
  — satisfying OSS-02's literal wording.
- **`README.md`**: adds a short "Contributing" section pointing at
  `CONTRIBUTING.md` and the `good first issue` label.
- **Six GitHub issues**, each labeled `good first issue`:
  - #9 Add a Notion service configuration
  - #10 Add a HubSpot service configuration
  - #11 Add an Airtable service configuration
  - #12 Add a Calendly service configuration
  - #13 `agentgate-verify`: add a `--format json` output option
  - #14 `agentgate-verify`: add a `--quiet` flag to suppress non-essential
    success output

  Four service-configuration issues, two verifier-output issues, exactly
  matching OSS-04's split. Every issue names the exact files to touch, a
  concrete acceptance checklist, and a `go test` command that needs no
  secrets, network access, or live third-party account (OSS-05).

## Key Design Decisions Realized

- **Notion, HubSpot, Airtable, and Calendly were chosen because their
  REST APIs fit the existing registry schema with zero Go code changes**
  — Bearer or OAuth2 auth, one HTTP method + path template per action.
  Linear (GraphQL-only), Trello (query-string key+token auth), and
  Discord (`Bot <token>` header format) were considered and rejected for
  not fitting the current `AuthCfg`/`Action` schema without a schema
  change that would make the issue silently more than "config-only."
- **Each service-config issue names all three parallel config-file
  locations** (`configs/services.yaml`, `config/services.yaml`,
  `configs/services/<name>.yaml`) discovered in Phase 9, and points a
  contributor at `TestProductionConfig_LocalDevDefaultMatches` as the
  check that would otherwise silently pass on an incomplete two-of-three
  edit.
- **Both verifier-output issues are additive, opt-in flags** with an
  explicit acceptance check that the existing default text output is
  byte-for-byte unchanged — avoiding a starter issue that could
  accidentally break an existing script or CI job already parsing
  `agentgate-verify`'s current output.

## Bugs Found During Implementation

None — this phase produced no Go code changes.

## Test Coverage Added

None — this phase is documentation and GitHub-issue-tracker content;
OSS-01 through OSS-05 are verified by inspection (see
`11-VERIFICATION.md`), matching Phase 10's precedent for a
documentation-only phase.

## Residual Risks / Follow-ups

- No CI/GitHub Actions workflow exists yet to automatically verify a
  contributor's PR — `CONTRIBUTING.md` says so explicitly rather than
  implying one exists.
- No issue or PR templates exist yet; not required by any OSS-0X item.
- If any of the four suggested service APIs (Notion, HubSpot, Airtable,
  Calendly) change their documented auth flow or endpoint paths after this
  phase, the corresponding issue's "suggested shape" section would need a
  correction — each issue links to the vendor's own current API docs so a
  contributor can verify against the live source, not just the issue text.

## Verification Commands Run

```
go build ./...      # unaffected; no Go files changed
go vet ./...         # unaffected
gofmt -l .            # clean, no output
```
