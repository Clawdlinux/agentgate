---
phase: 06-5-minute-quickstart-and-oss-launch
gathered: 2026-08-16
status: ready-for-planning
mode: autonomous (autopilot loop, no interactive user available)
research: PRD-receipts-oss.md TASK-R9, ROADMAP.md Phase 6 section
---

<domain>
## Phase Boundary

QST-01 through QST-05: README leads with a verified-receipt quickstart,
`docker compose up` needs no host Go toolchain, the documented flow
connects GitHub and verifies a receipt offline, state survives a
container restart, and the whole thing takes under five minutes for
someone who has never seen the repo. This is the OSS launch gate — Phases
7 through 11 follow it.

</domain>

<decisions>
## Blocking gap found during research: admin/OAuth routes were never mounted

`cmd/agentgw/main.go` has never mounted `internal/admin` or `internal/oauth`
— every prior phase's summary flagged this as an out-of-scope residual risk
because no LEDG-0X or VER-0X requirement touched it. QST-03 ("the
documented flow connects GitHub") makes it in-scope now: there is no way to
call `POST /admin/link` (get an authorize URL) or complete
`GET /auth/callback/{service}` without mounting these handlers. This phase
wires them into the composition root:

- `AGENTGATE_ADMIN_SECRET` becomes a required env var (fail closed, same
  pattern as `AGENTGATE_VAULT_KEY`) rather than defaulting silently.
- `AGENTGATE_PUBLIC_URL` (default `http://localhost:8080`) is the new env
  var for the OAuth callback base — nothing read one before.
- OAuth providers are built from the *registry*, not hardcoded per service:
  for every registered service with `auth.type: oauth2`, an env var pair
  `<SERVICE>_CLIENT_ID`/`<SERVICE>_CLIENT_SECRET` (already present as blank
  placeholders in `.env.example`) is read; a service without both values
  set is skipped with a warning, not a startup failure — an operator who
  only cares about GitHub shouldn't be forced to configure Slack and
  Stripe too.
- Routes added: `POST /admin/keys`, `DELETE /admin/keys/{id}`,
  `POST /admin/link`, `GET /admin/tokens/{user_id}` (all behind
  `RequireAdmin`), `GET /auth/callback/{service}` (public, matches
  GitHub's own redirect).

## docker-compose.yaml: bind mount, not a named volume

Switched `agentgate-data:/data` (an opaque Docker-managed volume) to a host
bind mount `./data:/data`. QST-03's "verifies its SQLite receipt offline"
step needs `agentgate-verify --source sqlite --path <file>` to reach the
database file directly from the host. A named volume would force an extra
`docker cp` or `docker compose exec` step just to reach the file — worse
for the five-minute budget, and less transparent for a new operator anyway.

## What could not be automated, and why: the real OAuth consent click

`POST /v1/act` against GitHub requires a token in the vault, which the
production flow only ever gets through a live OAuth authorization-code
exchange: `GetAuthorizeURL` returns a real `github.com/login/oauth/authorize`
URL, and completing it needs a registered GitHub OAuth App (Client
ID/Secret — created once via GitHub's web UI, no REST API exists to script
this) and a human clicking "Authorize" in a real browser session tied to a
real GitHub account.

This agent has one legitimately-owned credential in this environment: a
`gh` CLI session already authenticated as a real GitHub account with
`repo` scope (`gh auth token`). Driving a browser through GitHub's login
and consent screens on that account's behalf — or worse, creating a new
OAuth App via browser automation — is an account-modifying action on a
real user's GitHub identity that this agent will not take without
explicit authorization scoped to that specific action, separate from
"organize the repo" or "continue the roadmap." It is also inherently
non-reproducible fully automated CI (that is the point of OAuth consent).

**Resolution:** the README documents the real flow (register a GitHub
OAuth App once, click through consent) as what an actual operator does.
For this phase's own verification, the agent seeds the vault directly with
the real `gh auth token` value — functionally identical to what a completed
OAuth exchange produces (one valid access token for one user+service pair)
— to validate every other step against a genuine `api.github.com` response:
receipt creation, signing, persistence across restart, and offline
verification. This is recorded here and in `06-01-SUMMARY.md` rather than
silently presented as "the OAuth flow was tested end to end," which it was
not.

## README restructure

A new top-level `## Quickstart` section (four numbered steps: start,
bootstrap key and connect GitHub, call an action, verify) goes
immediately after the title and one-paragraph description, before
`## Architecture`. The existing content moves down unmodified except where
it references the vault-key/env-var names this phase's `main.go` fix
already corrected.

</decisions>

<code_context>
## Existing Code Insights

- `internal/registry.AuthCfg` already parses `authorize_url`, `token_url`,
  and `scopes` per service from YAML — `oauth.Provider` entries can be
  built directly from the registry instead of a second hardcoded table.
- `internal/admin.Handler.CreateKey`/`LinkAccount`/`ListTokens` and
  `internal/oauth.CallbackHandler.ServeHTTP`/`GetAuthorizeURL` are fully
  implemented and tested (`tests/integration/gateway_test.go` already
  exercises `CreateKey` and `LinkAccount` against a mux built the same way
  this phase's `main.go` now builds one) — this phase only mounts existing,
  working code into the production binary.
- Go 1.22+ `http.ServeMux` resolves the most specific registered pattern
  first, so registering `/admin/keys`, `/admin/link`, and
  `/auth/callback/{service}` directly on the same mux as `mux.Handle("/",
  srv)` is safe — none of them fall through to the gateway's own 404.

</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.

</specifics>

<deferred>
## Deferred Ideas

- CI/GitHub Actions wiring for `agentgate` and `agentgate-verify` builds:
  no QST-0X item requires it; Phase 11 (contributor entry path) is a more
  natural home if it becomes necessary.
- A programmatic or device-code OAuth alternative to the authorization-code
  flow: not requested by any requirement, and would be a real product
  design change, not a quickstart-documentation fix.

</deferred>
