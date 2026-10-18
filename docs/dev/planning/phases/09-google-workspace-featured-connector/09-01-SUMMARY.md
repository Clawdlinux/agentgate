---
phase: 09-google-workspace-featured-connector
plan: 09-01
completed: 2026-08-16
---

# Phase 9 Plan 01: Google Workspace Featured Connector — Summary

## What Was Built

- **`google_workspace` service entry** added to all three parallel
  service-config locations discovered during this phase (see
  `09-CONTEXT.md`): `configs/services.yaml` (the file the Dockerfile and
  quickstart actually load), `config/services.yaml` (the local, non-Docker
  dev default), and `configs/services/google_workspace.yaml` (the
  per-service reference file `README.md` points readers at).
  - `auth.type: oauth2`, `authorize_url: https://accounts.google.com/o/oauth2/v2/auth`,
    `token_url: https://oauth2.googleapis.com/token`,
    `scopes: [https://www.googleapis.com/auth/gmail.labels]` — the
    narrowest Gmail scope Google publishes (labels only, no message
    content access).
  - One action, `list_labels`: `GET /gmail/v1/users/me/labels`, no
    parameters — Gmail's own `me` convention for "the authenticated user"
    removes the need for a path parameter entirely.
- **`internal/registry/production_config_test.go`** (new): loads the real
  `configs/services.yaml` and `config/services.yaml` files directly — a
  previously entirely-missing contract test, since every prior
  `internal/registry` test used an inline fixture and none ever parsed the
  files this repo actually ships.
- **`pkg/sdk/helpers.go`**: added `Client.GoogleWorkspace`, matching the
  existing `Stripe`/`GitHub`/`Slack` convenience-helper pattern exactly.
  Covered in the existing `TestClient_Helpers` table.
- **`README.md`**: intro sentence, architecture diagram, the
  `allowed_services` example, the Go SDK usage example, and the
  environment-variable table now lead with GitHub/Slack/Google Workspace.
  Stripe's config, SDK helper, and tests are all unchanged — its README
  usage example is kept, explicitly labeled as "remains functional though
  unfeatured" rather than removed (CONN-04).

## Key Design Decisions Realized

- **No gateway code changes were needed at all.** `buildOAuthProviders`
  (registry-driven) and `injectAuth` (already sends `Authorization: Bearer
  <token>` for `oauth2`) both already generically support any
  registry-declared OAuth2 service. This phase is a pure
  configuration/test/documentation addition, matching its stated
  boundary.
- **Found and closed a real, pre-existing test gap**: three parallel
  service-config file locations existed with no automated check that any
  of them actually parsed, or that they stayed in sync with each other for
  the featured connector set. `TestProductionConfig_LoadsAndContainsFeaturedServices`,
  `TestProductionConfig_GoogleWorkspaceRequestsNarrowScope`, and
  `TestProductionConfig_LocalDevDefaultMatches` close this gap for all
  four current services (github, slack, google_workspace, stripe), not
  just the new one.

## Bugs Found During Implementation

- No implementation bugs. The one substantive discovery was
  process/documentation-level: three parallel, previously-unreconciled
  service-config files, none of which had ever been loaded by an
  automated test before this phase.

## Test Coverage Added

- `internal/registry/production_config_test.go`: the real
  `configs/services.yaml` parses and contains exactly github/slack/
  google_workspace (featured) plus stripe (functional, unfeatured);
  google_workspace's auth type and scope list match CONN-02 exactly; the
  real `config/services.yaml` (local dev default) contains the same four
  services, guarding against silent drift between the two files.
- `pkg/sdk/client_test.go`: `TestClient_Helpers` now also exercises
  `Client.GoogleWorkspace`.

## Residual Risks / Follow-ups

- `configs/services/*.yaml` per-service split files (referenced by
  `README.md` as documentation examples) have no automated check keeping
  them in sync with `configs/services.yaml`, since nothing in the running
  gateway loads them directly. Reconciling this is a real repo-hygiene
  improvement but is not required by any CONN-0X item.
- No CI/GitHub Actions build wiring yet — unchanged from Phases 6-8.

## Verification Commands Run

```
go build ./...
go vet ./...
gofmt -l .                          # clean, no output
go test ./... -race -count=1        # all packages pass
```
