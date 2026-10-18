---
phase: 11-contributor-entry-path
gathered: 2026-08-16
status: ready-for-planning
mode: autonomous (continuing established roadmap execution)
research: existing Makefile targets, go.mod (Go 1.25.0), local golangci-lint
  availability check, PRD-receipts-oss.md TASK-R11
---

<domain>
## Phase Boundary

OSS-01 through OSS-05: `CONTRIBUTING.md` documents prerequisites, build,
test, lint, focused PRs, and DCO sign-off (with the sign-off/signature
distinction stated explicitly); exactly six `good first issue`-labeled
issues exist, split 4 service-configuration tasks and 2 verifier-output
tasks, each naming files, acceptance checks, and a no-secrets test path.
This is the milestone's last phase.
</domain>

<decisions>
## Four new service configs chosen for a clean fit with the existing schema, not just popularity

Notion, HubSpot, Airtable, and Calendly were chosen specifically because
each has a REST API with straightforward Bearer or OAuth2 auth that maps
directly onto `internal/registry`'s existing `AuthCfg`/`Action` schema
with no changes to Go code. Candidates considered and rejected: Linear
(GraphQL-only, a single `/graphql` endpoint doesn't fit the
one-action-per-method-and-path-template model cleanly), Trello (key+token
passed as query parameters on every request, not a header — doesn't match
any of the three existing `auth.type` values without a schema change),
Discord (bot-token `Authorization: Bot <token>` header format, not the
plain `Bearer <token>` `injectAuth` currently sends for `oauth2`/`bearer`).
Choosing services that fit the *existing* schema keeps every one of these
four issues genuinely code-free, matching OSS-04's "service configuration"
framing rather than quietly requiring a registry schema change disguised
as a config-only task.

## Each service-config issue documents all three parallel config-file locations, not just one

Following Phase 9's discovery (three parallel `config(s)/services*` files:
the Docker-loaded merged file, the local dev default, and the per-service
reference files `README.md` points to), each of these four starter issues
explicitly names all three files a contributor must touch, and points at
`TestProductionConfig_LocalDevDefaultMatches` as the check that would
otherwise silently pass on an incomplete change (only one of the two real
files updated). This is deliberately more explicit than a generic
"add a service config" issue would be, to avoid a first-time contributor
discovering the three-file reality only after review feedback.

## Verifier-output issues are additive flags, never a default-behavior change

Both `--format json` and `--quiet` are opt-in: the existing default text
output must remain byte-for-byte unchanged. This was stated as an explicit
acceptance check in both issues, not left implicit, because a silent
default-output change would break any existing script or CI job already
parsing `agentgate-verify`'s current text format — the exact kind of
regression a contributor issue must not introduce.

## DCO sign-off vs. cryptographic signature gets its own explicit paragraph

`CONTRIBUTING.md`'s DCO section states plainly that `git commit -s` is not
a cryptographic signature (no GPG/SSH key, does not use `git commit -S`),
matching OSS-02's literal requirement. This distinction matters
specifically in a repository whose entire product claim is
cryptographically verifiable evidence (Ed25519-signed receipts) — using
imprecise language about "signing" commits in the same document that
teaches signed receipts would be a real, avoidable source of confusion.
</decisions>

<code_context>
## Existing Code Insights

- `Makefile` already defines `build`, `build-verify`, `run`, `test`,
  `docker`, `lint` — `CONTRIBUTING.md` documents these directly rather
  than inventing new ones.
- `golangci-lint` is not installed in this environment; `CONTRIBUTING.md`
  notes it as a separate install step rather than assuming it is already
  present, and lists the `go build`/`go vet`/`gofmt`/`go test` commands a
  contributor can run with only the Go toolchain, matching what this
  session has run before every phase's commit throughout this milestone.
- No CI/GitHub Actions workflow exists in this repository (unchanged
  since Phase 6) — `CONTRIBUTING.md` says so explicitly rather than
  implying a CI check will catch what a contributor skipped locally.
</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.
</specifics>

<deferred>
## Deferred Ideas

- A GitHub Actions CI workflow enforcing `go build`/`go vet`/`gofmt -l .`/
  `go test ./... -race` on every PR — not required by any OSS-0X item, and
  a real, separate piece of work (choosing a runner, caching Go modules,
  branch protection rules) beyond this phase's stated boundary.
- Issue and PR templates (`.github/ISSUE_TEMPLATE/`,
  `.github/PULL_REQUEST_TEMPLATE.md`) — not required by any OSS-0X item.
</deferred>
