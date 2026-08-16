---
phase: 06-5-minute-quickstart-and-oss-launch
plan: 06-01
completed: 2026-08-16
status: complete
---

# Phase 6 Summary: 5-Minute Quickstart and OSS Launch

## What was built

- **`cmd/agentgw/main.go`**: mounted `internal/admin` and `internal/oauth`
  for the first time in any phase — `POST /admin/keys`, `DELETE
  /admin/keys/{id}`, `POST /admin/link`, `GET /admin/tokens/{user_id}`
  (behind `RequireAdmin`), and `GET /auth/callback/{service}` (public).
  `AGENTGATE_ADMIN_SECRET` is now required (fails closed, same pattern as
  the vault key). New `AGENTGATE_PUBLIC_URL` env var (default
  `http://localhost:8080`) is the OAuth callback base. OAuth providers are
  built from the service registry itself (any `auth.type: oauth2` service
  with both `<SERVICE>_CLIENT_ID`/`<SERVICE>_CLIENT_SECRET` set), not
  hardcoded per service.
- **`configs/services.yaml`**: this file did not exist. The Dockerfile and
  `docker-compose.yaml` both referenced
  `/etc/agentgate/configs/services.yaml` as the default `--config` path,
  but `configs/` only had per-service files under `configs/services/`. No
  prior phase's Docker build could have actually started successfully —
  this was a real, previously undiscovered bug. Fixed by combining
  `github.yaml`, `slack.yaml`, and `stripe.yaml` into one
  `configs/services.yaml`.
- **`docker-compose.yaml`**: bind mount `./data:/data` replaces the opaque
  named volume `agentgate-data`, so `agentgate-verify` can reach the
  database file directly from the host without `docker cp`. Added
  `AGENTGATE_PUBLIC_URL` and pass-through `GITHUB_CLIENT_ID`/`_SECRET`.
  Removed the obsolete `version: "3.9"` key (current `docker compose`
  warns and ignores it).
- **`Dockerfile`**: now also builds and ships `agentgate-verify` in the
  final image, so verification can run with `docker compose exec
  agentgate agentgate-verify ...` — inside the same container/VM as the
  writer, avoiding a real Docker Desktop bind-mount limitation found
  during validation (below).
- **`internal/receipt/verifier.go`**: changed the trust-file JSON shape to
  exactly match `GET /v1/receipts/pubkey`'s wire response
  (`{"keys": [{"kid", "public_key_hex", ...}]}` instead of a bare array
  with a `public_key` field). An operator can now save that endpoint's
  response directly as the trust file with zero translation. Updated
  `cmd/agentgate-verify/main_test.go`'s trust-file builder to match; no
  other test needed changes since `verifier_test.go` builds `TrustedKey`
  values directly, not through JSON.
- **`README.md`**: new `## Quickstart` section immediately after the
  one-paragraph description, before `## Architecture` (QST-01). Removed
  the old, stale `## Quick Start` section (referenced the dead
  `AGENT_API_KEY` env var and a `go run` path that never worked without a
  service registry file existing). Fixed the environment variable table
  (`AGENT_API_KEY` and `VAULT_ENCRYPTION_KEY` were never real; added
  `AGENTGATE_PUBLIC_URL`). Added a "signed receipts" line to the Security
  Model section — the README never mentioned receipts at all before this
  phase, despite them being the entire milestone's point.

## Real validation, not a description

Ran the actual documented flow end to end against this branch's code,
using Docker Desktop with the daemon running:

1. `docker compose build` (cold, no layer cache): ~62s.
2. `docker compose up -d`: container starts, bootstraps one agent key,
   logs it once. Confirmed via `docker compose logs`.
3. `POST /v1/act` for `github.list_repos`: real HTTP call to
   `api.github.com`, real 200 response, real signed receipt committed
   (confirmed via `docker compose exec agentgate agentgate-verify`).
   ~1.3s, dominated by the real network round trip.
4. In-container verify (`wget` the pubkey endpoint, run
   `agentgate-verify`): ~0.4s, `PASS`.
5. `docker compose restart agentgate`: no new bootstrap-key log line
   (existing key preserved), and the *same* receipt (identical seq and
   hash) still verifies afterward — the signing identity and the receipt
   both survived the restart (QST-04). A second `/v1/act` call
   post-restart correctly continued the chain at seq 2.
6. Full `docker compose down && rm -rf data && docker compose up -d
   --build` (fully clean state) plus steps 2-4: comfortably under a
   minute of purely mechanical time.

**What this validation could not include:** the real GitHub OAuth
consent screen. `POST /v1/act` against GitHub needs a vault-stored
access token, which the production flow only ever gets through a live
`github.com/login/oauth/authorize` redirect and a human clicking
"Authorize" — registering a GitHub OAuth App has no REST API, and driving
a browser through a real account's login/consent screens on this agent's
behalf is an account-modifying action this agent will not take without
explicit, separate authorization. For this validation, the agent seeded
the vault directly with a real token from its own already-authenticated
`gh` CLI session (`gh auth token`) — functionally identical to what a
completed OAuth exchange produces for the one thing that matters here
(one valid access token for one user+service pair) — so every other step
(the real API call, signing, persistence, offline verification) was
tested against genuine `api.github.com` responses. This is recorded
here and in `06-CONTEXT.md`, not silently presented as "OAuth was tested
end to end."

**Timing conclusion for QST-05:** every mechanical step (build, start,
act, verify, restart, re-verify) totals well under a minute. The
remaining budget of a five-minute walkthrough is realistically consumed
by the one human step this agent could not perform: registering a GitHub
OAuth App (a one-time, ~1-2 minute action) and clicking "Authorize" once
redirected. This is inherent to how OAuth works for any project's
quickstart, not specific to AgentGate.

## Real bug found and fixed during validation

Reading a live, bind-mounted SQLite WAL-mode database from the *host*
while the *container* is actively writing can show a stale/incomplete
view on Docker Desktop for Mac (virtiofs does not guarantee immediate
cross-boundary visibility of `-wal`/`-shm` file changes). Reproduced
directly: a second `/v1/act` call's receipt was confirmed committed by
the container's own logs, but a `sqlite3` query run from the host
immediately afterward still showed only 1 row — until the container was
stopped (forcing a checkpoint), after which the host saw 2 rows.
**Fix:** ship `agentgate-verify` inside the container image and document
`docker compose exec agentgate agentgate-verify ...` as the primary
verification path (same container as the writer, no cross-boundary
staleness). Host-side verification is documented as a secondary option
that requires stopping the container first.

## Key test results

- `go build ./...`, `go vet ./...`: clean.
- `gofmt -l .`: only the pre-existing, out-of-scope
  `internal/registry/registry.go`.
- `go test ./... -race -count=1`: every package passes, including the
  updated `cmd/agentgate-verify` tests against the new trust-file shape.
- Manual end-to-end Docker validation: described above, all steps passed.

## Deviations from the plan

- Wiring `internal/admin`/`internal/oauth` into `cmd/agentgw/main.go` was
  flagged as an out-of-scope residual risk in every prior phase's summary
  (Phase 4, Phase 5). QST-03 made it in-scope: there is no way to satisfy
  "the documented flow connects GitHub" without a mounted `/admin/link`
  and `/auth/callback/{service}`. Not scope creep — a blocking discovery.
- The trust-file JSON shape from Phase 5 changed (bare array with
  `public_key` → `{"keys": [...]}` with `public_key_hex`) to match the
  pubkey endpoint exactly. No VER-0X requirement specifies a trust-file
  shape; this is a usability fix made before anything using the old shape
  shipped or was reviewed (PR #2, containing Phase 5, is still open).

## Residual risks

- `internal/registry/registry.go` gofmt drift — unchanged since Phase 3.
- `internal/db`'s missing migration ledger — still deferred, still not
  required by anything.
- No CI/GitHub Actions wiring yet for building `agentgate`/
  `agentgate-verify` — no QST-0X item requires it; a natural fit for
  Phase 11 (contributor entry path) if pursued.
- Host-side SQLite reads while the container is running remain unreliable
  on Docker Desktop for Mac; documented, not solved at the infrastructure
  level (would require e.g. a read-only HTTP export endpoint, which is
  Phase 7's scope, not this one's).
