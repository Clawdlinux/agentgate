---
phase: 09-google-workspace-featured-connector
gathered: 2026-08-16
status: ready-for-planning
mode: autonomous (continuing established roadmap execution)
research: PRD-receipts-oss.md TASK-R8; live inspection of this repo's own
  three parallel service-config locations (found during this phase)
---

<domain>
## Phase Boundary

CONN-01 through CONN-04: an operator can call one narrow Gmail labels
action through the existing registry and OAuth flow; launch docs feature
GitHub, Slack, and Google Workspace; Stripe stays configured and
functional but unfeatured. Sourced comparison (Phase 10) and the
contribution path (Phase 11) are untouched.
</domain>

<decisions>
## Three parallel service-config locations already existed; found before writing anything

Before touching any file, `grep` for every reference to `config(s)/services`
turned up three, not one:

- `configs/services.yaml` (plural directory, one merged file) — the file
  the Dockerfile's `CMD` actually points at
  (`/etc/agentgate/configs/services.yaml`) and the one Phase 6's quickstart
  validated live against a running Docker daemon. Confirmed via
  `06-01-SUMMARY.md`: this file did not even exist before Phase 6, which
  created it because the Dockerfile referenced a path nothing produced.
- `config/services.yaml` (singular directory) — only ever the *default*
  value of `cmd/agentgw`'s `--config` flag, for a host `go run` without
  Docker. Not touched by any container path.
- `configs/services/{github,slack,stripe}.yaml` — per-service split files
  that `README.md` points readers at as documentation/examples
  (`"See configs/services/github.yaml for an example"`), but that nothing
  in `cmd/agentgw` or the Dockerfile ever loads directly.

All three were updated to add `google_workspace`, so none of them silently
drift out of sync with each other for the featured connector set. A new
test, `TestProductionConfig_LocalDevDefaultMatches`, exists specifically
to catch that drift for `config/services.yaml` going forward; no
equivalent automated check exists yet for the `configs/services/*.yaml`
split files, since nothing loads them and PRD Non-Goals rule out inventing
new tooling to enforce doc-example freshness.

## A previously-missing contract test: nothing had ever loaded the real production config file

Every existing `internal/registry` test used an inline YAML fixture; none
ever parsed `configs/services.yaml` itself. `TestProductionConfig_*`
closes that gap generally (not just for Google Workspace): it now asserts
the real file parses, contains exactly the three featured services plus
Stripe, and that Google Workspace's OAuth scope is exactly the narrow
`gmail.labels` grant, not a broader Gmail scope.

## One action: `list_labels`, no parameters

Gmail's `users.labels.list` endpoint (`GET /gmail/v1/users/{userId}/labels`)
takes no query parameters and returns every label for the specified user
in one call. Using the literal path segment `me` (Gmail API's
own convention for "the authenticated user," matching how `on_behalf_of`
already ties the call to one specific human) removes the need for a
`userId` path parameter entirely — the simplest form CONN-01 ("one narrow
Gmail labels action") can take.

## `Auth.Scopes` carries exactly one entry

`https://www.googleapis.com/auth/gmail.labels` is Google's own narrowest
labels-only Gmail scope (create/read/update/delete labels; no message
content access). No broader Gmail scope is requested, matching CONN-02
directly.

## Stripe: config, SDK helper, and tests unchanged; only launch docs move

CONN-04 asks for Stripe to remain "functional but unfeatured," not
removed. `configs/services.yaml`'s `stripe` block, `pkg/sdk.Client.Stripe`,
and their existing tests are untouched. Only `README.md`'s intro sentence,
architecture diagram, `allowed_services` example, and the Go SDK usage
example were edited to lead with GitHub/Slack/Google Workspace, with
Stripe usage kept as a clearly-labeled "remains functional though
unfeatured" example rather than deleted.
</decisions>

<code_context>
## Existing Code Insights

- `cmd/agentgw/main.go`'s `buildOAuthProviders` is fully registry-driven:
  it loops over every registered service whose `auth.type == "oauth2"` and
  looks for `<UPPERCASE_SERVICE_NAME>_CLIENT_ID`/`_CLIENT_SECRET` env vars.
  Adding Google Workspace requires zero changes to this function — it
  picks up `google_workspace` automatically once the config exists, using
  `GOOGLE_WORKSPACE_CLIENT_ID`/`GOOGLE_WORKSPACE_CLIENT_SECRET`.
- `internal/gateway/gateway.go`'s `injectAuth` already sends
  `Authorization: Bearer <token>` for `auth.type: oauth2`, which is exactly
  what the Gmail API expects. No gateway code changes were needed for this
  phase at all — it is a pure configuration, test, and documentation
  addition, matching the phase's stated scope.
</code_context>

<specifics>
## Specific Ideas

None beyond the decisions above.
</specifics>

<deferred>
## Deferred Ideas

- Reconciling `configs/services/*.yaml`'s split-file convention against
  the merged `configs/services.yaml` file that actually ships (e.g. a
  build step that generates one from the other) — a genuine repo-hygiene
  improvement, but not required by any CONN-0X item and out of this
  phase's scope.
- Additional Gmail actions beyond `list_labels` (e.g. reading or sending
  messages) — CONN-01 asks for exactly one narrow action.
</deferred>
