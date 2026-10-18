# AgentGate

[![CI](https://github.com/Clawdlinux/agentgate/actions/workflows/ci.yml/badge.svg)](https://github.com/Clawdlinux/agentgate/actions/workflows/ci.yml)

A thin API gateway that lets AI agents call SaaS APIs (GitHub, Slack, Google Workspace) on behalf of users. Agents never see tokens. The gateway handles OAuth, encrypted token storage, and request proxying. Recorded actions get signed receipts that anyone can verify offline, without AgentGate's secret key.

## Quickstart

**Run the released image.** No clone, no build, no Go toolchain — just the
image published to GHCR on every tagged release:

```bash
umask 077
printf 'AGENTGATE_VAULT_KEY=%s\nAGENTGATE_ADMIN_SECRET=%s\n' \
  "$(openssl rand -base64 24)" "$(openssl rand -base64 32)" > .env
mkdir -p data
docker run -d --name agentgate \
  -p 8080:8080 \
  --env-file .env \
  -v $(pwd)/data:/data \
  ghcr.io/clawdlinux/agentgate:latest
```

Keep `.env` private. Keep it when you recreate the container. The vault cannot
decrypt existing records without the same key.

It bootstraps one agent API key on first boot and logs it once:

```bash
docker logs agentgate | grep agent_key
# {"agent_key":"agk_<keyid>_<secret>"}
# Save this. It is never shown again.
```

New keys use lowercase hex: `agk_<keyid>_<secret>`. The ID contains 16 random bytes. The secret contains 32 random bytes.
Existing `ag_live_` keys still work, but are deprecated. Replace them through `POST /admin/keys`.
See [API key storage and migration](docs/api-keys.md) for details.

**Call an action.** Without a linked account this returns `token_missing` —
the point being that a receipt is still committed for the *attempt*, not
just for successful calls, so the audit trail can't have quiet gaps:

```bash
curl -s -X POST http://localhost:8080/v1/act \
  -H "Authorization: Bearer <agent-key-from-above>" \
  -H "Content-Type: application/json" \
  -d '{"service":"github","action":"list_repos","on_behalf_of":"demo-user","params":{"per_page":1}}'
# {"error":"no token for user demo-user on service github — user must connect their account first","code":"token_missing"}
```

**Connect a real account.** Register a GitHub OAuth App once at
[github.com/settings/developers](https://github.com/settings/developers)
(callback URL `http://localhost:8080/auth/callback/github`), pass
`GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET` as extra `-e` flags on the `docker
run` above, then get the authorization link and open it in a browser:

```bash
curl -s -X POST http://localhost:8080/admin/link \
  -H "X-Admin-Secret: admin-dev-secret-change-me!!" \
  -H "Content-Type: application/json" \
  -d '{"user_id":"demo-user","service":"github"}'
# open the returned authorize_url, click Authorize, then re-run the /v1/act call above
```

**Inspect the receipt ledger.** Open
[`http://localhost:8080/dashboard/`](http://localhost:8080/dashboard/) and
enter the gateway URL, then the value of `AGENTGATE_ADMIN_SECRET`. The
dashboard checks the visible receipt chain locally. Use the standalone
verifier below for full cryptographic proof.

### Receipt durability

The ledger commits immediately by default, batching up to 256 requests that are already waiting.
Requests arriving during a commit form the next batch.
A single writer signs each batch inside one `BEGIN IMMEDIATE` transaction.
It reads the committed head once and commits every insert before replying.
Invalid drafts fail separately. A failed transaction consumes no sequence numbers.
After acquiring SQLite's write lock, the writer reads the head and drops cancelled requests before signing.
After that check, a receipt can commit even if its caller stops waiting.
The gateway never returns a successful action response without a committed receipt.

The signer caches its decrypted active key in memory.
Run a single gateway writer per database. Rotation from another process is unsupported.
Rotation must start at the next committed sequence. A stale or future boundary fails without changing the key.
Shutdown stops HTTP first, then closes the ledger and clears the signer's cached private key.
`receipt.WithBatchDelay` and `receipt.WithBatchSize` configure batching for embedded gateways.
The default delay is 0. A positive delay adds a maximum collection window before each commit.

### Auditing, standalone

Verification never depends on the gateway, Docker, or a Go toolchain — it
reads the SQLite receipt log and a pinned trust file directly. Anyone with a
copy of `agentgate.db` and a trust file can run this, including someone who
has never seen this repo:

```bash
# while the gateway is up, once: save the trust root as a local file
curl -s http://localhost:8080/v1/receipts/pubkey -o trust.json

# download the verifier archive matching your OS/arch from
# https://github.com/Clawdlinux/agentgate/releases/latest — no other install step
tar xzf agentgate_<version>_<os>_<arch>.tar.gz

./agentgate-verify --source sqlite --path ./data/agentgate.db --trust-root ./trust.json
# PASS: 1 receipts verified, head seq=1 hash=...
```

No network call happens during verification itself — `trust.json` is a
pinned local file, and `agentgate-verify` only reads the local SQLite file.
Pass `--expected-head <seq>:<hash>` (from a checkpoint recorded separately,
e.g. at handoff to an auditor) to also assert completeness, not just chain
integrity. For scripts, add `--format json` to receive one machine-readable
result object while keeping the same exit codes. Add `--quiet` (or `-q`) to
text output to print only the `PASS:` summary on successful verification.

### Verify receipts from other producers

The frozen [receipt v1 specification](docs/spec/RECEIPT-v1.md) defines the signed bytes and verification rules.

The receipt protocol lives in a public, standard-library-only Go package:
[`pkg/receiptspec`](pkg/receiptspec). It holds the receipt type, canonical
hashing, Ed25519 signing, chain and export verification, and JSONL encoding.
The gateway and `agentgate-verify` both use it, so there is one
implementation. Another producer, such as the Clawdlinux operator, builds a
chain with `receiptspec.NewChain` and any `agentgate-verify` checks it with
`--source jsonl`. To record decisions instead of API calls, see
[`pkg/receiptspec/DECISION.md`](pkg/receiptspec/DECISION.md).

### Development option

To build from source instead of pulling the release image — useful when
iterating on the gateway itself — `docker compose` still works and rebuilds
the image on every `up`:

```bash
git clone https://github.com/Clawdlinux/agentgate.git
cd agentgate
docker compose up -d --build
```

It reads OAuth credentials from a `.env` file (see `.env.example`) instead
of inline `-e` flags, and the same `/v1/act` and `/admin/link` calls above
work against it unchanged.

The compose image is also built `FROM scratch` (no shell, no package
manager — see the comment in `Dockerfile`), so verifying "inside" the
container isn't an option here either. Use the same standalone flow from
the Auditing section above, pointed at compose's bind-mounted
`./data/agentgate.db`: `curl` the trust root, download or `make
build-verify` the verifier, and run it against the host path directly.

Stop and restart the container (`docker compose restart agentgate`) and
run the verify command again — the signing identity and the receipt are
still there, from the bind-mounted `./data/agentgate.db`.

## Architecture

```text
Agent: POST /v1/act
  |
  v
API key authentication (indexed HMAC lookup, verified-key cache)
  |
  v
Body and params validation (1 MiB read cap, JCS digest)
  |
  v
Key rate limit -> Service and principal scopes -> Tenant and service rate limits
  |
  v
Delegation verification, if presented (token shape caps, bounded Datalog)
  |
  v
Registry lookup -> Escaped path parameters and URL validation
  |
  v
Vault (bounded token cache, singleflight OAuth refresh)
  |
  v
Credential injection -> Tenant concurrency admission
  |
  v
Upstream pool (circuit breaker, bounded retries, same-origin redirects)
  |
  v
Bounded response body or SSE response headers
  |
  v
Group-commit receipt ledger (signed SQLite transaction)
  |
  v
JSON response or bounded SSE stream
```

Authentication runs inside the gateway, not standalone middleware.
Legacy keys retain a bcrypt fallback. See [API key storage](docs/api-keys.md).
Every authenticated, schema-valid attempt reaches the receipt ledger, including policy denials.
Invalid credentials and malformed bodies return before receipt creation.
Receipt commit failures suppress the action response.
SSE commits a header-time receipt before forwarding events. It does not certify stream completion.
See [upstream limits](#upstream-connections-and-retries) for body caps, retry rules, and stream limits.

Historical build instructions live in [docs/dev/](docs/dev/).
Planning history lives in [docs/dev/planning/](docs/dev/planning/).

## API Reference

### POST /v1/act

Execute a SaaS API action on behalf of a user.

**Request:**
```json
{
  "service": "github",
  "action": "list_repos",
  "on_behalf_of": "user-42",
  "params": {"type": "owner", "sort": "updated"}
}
```

**Headers:** `Authorization: Bearer <agent-api-key>`

**Response (success):**
```json
{
  "status": 200,
  "body": [{"id": 1, "name": "my-repo"}],
  "latency_ms": 142
}
```

**Response (error):**
```json
{
  "error": "no token for user user-42 on service github",
  "code": "token_missing"
}
```

### GET /v1/services

List available services.

### GET /v1/services/{name}

Describe a service and its actions.

### GET /healthz

Health check endpoint.

### Admin Endpoints

All admin endpoints require `X-Admin-Secret` header.

#### POST /admin/keys
Create a new agent API key.
```json
{"name": "my-agent", "allowed_services": ["github", "slack"], "allowed_users": ["user-42"]}
```

#### DELETE /admin/keys/{id}
Revoke an API key.

#### POST /admin/link
Get OAuth authorization URL for user account linking.
```json
{"user_id": "user-42", "service": "github"}
```

#### POST /admin/tokens
Connect a bearer token for Slack, Stripe, or Calendly. The token is encrypted
in the vault and never returned.
```json
{"user_id": "user-42", "service": "stripe", "access_token": "<stripe-token>"}
```

#### GET /admin/tokens/{user_id}
List linked services for a user (no token values exposed).

### OAuth Callback

#### GET /auth/callback/{service}
OAuth redirect handler — exchanges code for tokens, stores encrypted in vault.

## Go SDK

```go
import "github.com/Clawdlinux/agentgate/pkg/sdk"

client := sdk.NewClient("http://localhost:8080", "agk_<keyid>_<secret>")

// Call any service
resp, err := client.Act(ctx, sdk.ActRequest{
    Service:    "github",
    Action:     "list_repos",
    OnBehalfOf: "user-42",
})

// Convenience helpers
resp, err := client.GitHub(ctx, "user-42", "list_repos", nil)
resp, err := client.Slack(ctx, "user-42", "post_message", map[string]interface{}{"channel": "#general", "text": "Hello"})
resp, err := client.Act(ctx, sdk.ActRequest{Service: "google_workspace", Action: "list_labels", OnBehalfOf: "user-42"})

// Stripe remains fully functional though unfeatured at launch:
resp, err := client.Stripe(ctx, "user-42", "list_invoices", map[string]interface{}{"limit": 10})

// Error handling
if sdk.IsTokenMissing(err) {
    // User needs to link their account
}
if sdk.IsRateLimited(err) {
    // Back off and retry
}
```

## Invariant layer inside Clawdlinux

AgentGate is deterministic. Agents never see tokens, calls go only to configured services, and every action attempt commits a signed receipt. Clawdlinux treats rules like these as its invariant layer, which no model can override. AgentGate makes no decision-model claims. See the [decision architecture](https://github.com/Clawdlinux/agentic-operator-core/blob/main/docs/architecture/decision-architecture.md).

## Security Model

1. **Agent keys** are scoped (service × user). Agents can only access what's explicitly granted.
2. **Tokens encrypted at rest.** AES-256-GCM with 32-byte key from environment.
3. **No token exposure.** Agents never see OAuth tokens — only the gateway touches them.
4. **Signed receipts.** Every authenticated action attempt commits one Ed25519-signed, hash-chained receipt before the response is returned — verify offline with `agentgate-verify`, no gateway state or private key needed.
5. **Rate limiting.** Key admission precedes scopes. Tenant and optional service buckets follow scopes.
6. **OAuth state encrypted** with AES-256-GCM and expires after 10 minutes.
7. **OAuth refresh.** Configured OAuth providers refresh tokens expiring within 5 minutes before dispatch. A failed refresh returns `token_expired` without calling the SaaS API.

### Vault cache and refresh

Vault reads cache decrypted access and refresh tokens in process memory.
The default TTL is `60s`, with an LRU limit of 10,000 user-service pairs.
An entry expires earlier when its token reaches 30 seconds before expiry.
Tokens without an expiry use the TTL alone.
Tokens already within that 30-second window aren't cached.

Token strings cannot be reliably zeroed. Keep the cache bounded.
Delete drops cached references. Go may retain string bytes until garbage collection.
Expired entries aren't reused. Lookup or eviction removes their cached references.
Put writes SQLite before caching the new token. Failed writes invalidate the local entry.
All writes must pass through the decorated store.

Concurrent refreshes share one result per store and user-service pair within a process.
Current waiters share refresh failures. Later calls retry.
Canceling a waiter doesn't cancel the shared refresh.
Each shared refresh has a separate `15s` deadline. OAuth HTTP exchanges also have a `15s` client timeout.
Refresh functions receive that context and must honor cancellation.
Successful exchanges still persist their results after the refresh deadline. That deadline doesn't cancel persistence retries.
Shared refresh panics return a fixed error without panic values. The flight is cleared so later requests can retry.
Timeouts and canceled waits return HTTP `504` with receipt error code `vault_refresh_timeout`, not `token_expired`.
Refresh writes compare the stored refresh token inside a SQLite transaction.
Deleted credentials stay deleted. A concurrent re-link keeps its replacement token and scopes.
On a comparison mismatch, refresh returns the current credential only when it remains valid.
Rotated refresh tokens get up to 3 conditional write attempts.
If every write fails, the current request still receives the new access token.
The failure is logged without token values. Later requests may require re-linking because the rotation wasn't saved.
Replicas maintain separate caches and refresh independently.
Rotating refresh tokens still need cross-process coordination when replicas share credentials.
External writes or deletes can leave stale local credentials until cache expiry.
Set `AGENTGATE_VAULT_CACHE_TTL=0` to disable plaintext caching. Refresh sharing remains enabled.

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `AGENTGATE_VAULT_KEY` | 32-byte vault key. Also derives receipt encryption keys and the API-key HMAC pepper. | Yes |
| `AGENTGATE_VAULT_CACHE_TTL` | Decrypted-token cache duration. Default `60s`; `0` disables caching. Negative or invalid durations stop startup. | No |
| `AGENTGATE_ADMIN_SECRET` | Secret for admin API access | Yes |
| `AGENTGATE_PUBLIC_URL` | Public base URL for OAuth callbacks and admin cookie security. Default `http://localhost:8080`. | No |
| `AGENTGATE_API_KEY_CACHE_TTL` | Verified-key cache duration. Default `30s`; `0` disables caching. | No |
| `AGENTGATE_RATE_KEY_RPS` | Requests per second per API key ID. Default `20`. Zero requires tenant admission disabled. | No |
| `AGENTGATE_RATE_KEY_BURST` | Per-key token bucket burst. Default `40`. Zero requires tenant admission disabled. | No |
| `AGENTGATE_RATE_TENANT_RPS` | Requests per second per tenant. Default `100`; `0` disables this bucket. | No |
| `AGENTGATE_RATE_TENANT_BURST` | Per-tenant token bucket burst. Default `200`; `0` disables this bucket. | No |
| `AGENTGATE_TENANT_MAX_INFLIGHT` | In-flight upstream requests per tenant. Default `32`; `0` disables this cap. | No |
| `AGENTGATE_BISCUIT_MAX_DURATION` | Go duration per Biscuit Datalog world run. Default `250ms`; range `1ms` through `5s`, inclusive. Invalid values stop startup. | No |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | GitHub OAuth credentials | For GitHub OAuth |
| `SLACK_CLIENT_ID` / `SLACK_CLIENT_SECRET` | Slack OAuth credentials | For Slack OAuth |
| `GOOGLE_WORKSPACE_CLIENT_ID` / `GOOGLE_WORKSPACE_CLIENT_SECRET` | Google OAuth credentials, requesting only the narrow `gmail.labels` scope | For Google Workspace OAuth |
| `STRIPE_CLIENT_ID` / `STRIPE_CLIENT_SECRET` | Stripe OAuth credentials (Stripe remains configured and functional, just not a featured launch connector) | For Stripe OAuth |

Biscuit verification caps each world run at 1000 facts and 100 iterations.
These caps stay in code. The duration applies during authorization and the later grant query.
Issued tokens contain 1 grant fact and no rules. Typical attenuation adds a few checks.
The `250ms` default tolerates scheduler delays while keeping those work caps unchanged.
Limit hits deny the request. `errors.Is` matches `delegation.ErrDenied` and the corresponding `datalog.ErrWorldRunLimit*` cause.

Agent API keys are bootstrapped on first boot and logged once. There is no environment variable for a pre-supplied key.
SQLite stores a public ID and a peppered HMAC-SHA256, never the secret. Use `POST /admin/keys` to create more.
Legacy `ag_live_` rows keep their bcrypt hashes. Successful validation adds an indexed HMAC without changing the presented key.
Revoked rows never enter legacy bcrypt scans.

#### Request admission

API keys have no organization ID. The organization store binds admins, not agent keys.
For explicitly scoped keys, tenant identity is the authorized `on_behalf_of` principal.
Such keys share that principal's rate bucket and concurrency cap.
Each key's bucket spans services and human principals.
Wildcard keys allowing `"*"` use 1 key-scoped tenant for every principal.
Different wildcard keys don't share human quotas.
Each key selects at most 64 tenant identities from its fixed scope list.
When the list exceeds 64 entries, only the first 63 retain shared human identity.
The remaining principals share 1 key-scoped fallback tenant.
Human and fallback identities use separate namespaces. No per-key tenant-tracking map is needed.

Rate settings accept finite, non-negative numbers. Bursts and concurrency caps require integers.
Invalid environment values stop startup. Either zero rate or zero burst disables that bucket.
Both key settings must stay positive while tenant rate limiting or tenant concurrency is enabled.
Per-service limits are optional. Set `rate_limit` under a service in the loaded registry YAML.
They apply per API key and service, alongside the global limits.
See [registry rate limits](docs/registry.md#rate-limits) for YAML examples.

The key bucket runs after API key authentication and body validation.
In-memory service and principal scope checks run next.
Scope denials consume only key tokens. Their receipts retain `scope_denied`.
Tenant and service buckets run after scope checks, before delegation verification or vault access.
Rate denials return HTTP 429 with `rate_limited`.
`Retry-After` rounds the reservation delay up to whole seconds, with a minimum of 1.
Concurrency admission runs immediately before upstream dispatch.
Full tenants return HTTP 429 with `concurrency_limited` and `Retry-After: 1`.
Concurrency rejection refunds the key, tenant, and service charges without losing elapsed refill.
Slots remain held through response completion, including SSE streams and receipt commits.
Failures, disconnects, stream termination, and dispatch panics release them.
Every authenticated, schema-valid attempt still commits a receipt before its response, including admission denials.
Both admission errors record policy `rate_limited`. Receipt failures return `receipt_failed` instead.

Admission state is process-local. Replicas don't share quotas.
The combined bucket and semaphore cache holds at most 100000 entries.
Requests trigger idle sweeps at most once per minute. Entries idle for 10 minutes become eviction candidates.
Only full buckets without pending permits or in-flight requests can be evicted.
Slow buckets retain their quota until fully refilled.
At capacity, admission scans up to 64 entries from the LRU tail.
Pinned or depleted entries move to the front. The first evictable entry is replaced.
If none qualifies, admission fails closed with HTTP 429 and `Retry-After: 1`.
Restart the gateway after changing admission settings or service YAML.

#### Biscuit token security caps

Tokens are limited to 4096 bytes and 8 attenuation blocks.
Authority contains exactly 1 `grant` fact with 4 strings and an expiry date. Authority rules are rejected.
Attenuation blocks can contain checks only. Facts and rules are rejected.
Each block permits at most 16 checks. Each check query body permits at most 4 predicates.
Each query permits at most 8 expressions. Each expression permits at most 32 operations.
Regex `.matches()` expressions are rejected because their compile and match work bypasses Datalog run limits.
Authority checks follow the same caps.

These checks run before authorizer construction. Rejections match both `delegation.ErrDenied` and `delegation.ErrTokenShape`.
Errors contain fixed reasons, not token contents.
The early rate limiter runs before these checks. Structural caps then bound fact joins and block processing.
Biscuit-go v2.2.0 deadlines do not cover check evaluation and cannot reliably stop timed-out goroutines.
Runtime limits alone do not bound per-request CPU.

### Admin Session Cookies

Set `AGENTGATE_PUBLIC_URL` to your HTTPS address behind a TLS-terminating proxy.
Admin cookies default to `Secure`, `HttpOnly`, and `SameSite=Strict`, with a 1-hour lifetime and `Path=/`.
Only explicit HTTP URLs on `localhost` or `127.0.0.1` disable `Secure` for local development.
Direct TLS requests always set `Secure`, including when the public URL uses local HTTP.
Forwarded headers cannot downgrade cookie security.
Logout clears the cookie with the same security attributes.
OAuth callbacks validate encrypted state without an admin session, so they don't need `SameSite=Lax`.

### Service Configs

Service configurations are YAML files in `configs/services/`. See `configs/services/google_workspace.yaml` for an example. The gateway itself loads the merged `configs/services.yaml`.

### Upstream connections and retries

Each service gets a separate HTTP client, connection pool, and circuit breaker.
Existing service YAML needs no changes. Optional `upstream` settings override defaults.
Settings take effect on the service's first dispatch. Restart the gateway after changing them.

```yaml
services:
  example:
    base_url: https://api.example.com
    auth:
      type: bearer
    upstream:
      request_timeout: 30s
      response_header_timeout: 10s
      max_idle_conns_per_host: 64
      max_conns_per_host: 0
      max_response_bytes: 1048576
      max_stream_bytes: 67108864
      stream_idle_timeout: 60s
      retry:
        max_retries: 2
        base_delay: 100ms
        max_delay: 2s
      breaker:
        failure_threshold: 5
        cooldown: 30s
    actions:
      list:
        method: GET
        path: /items
      replace:
        method: PUT
        path: /items/{id}
        idempotent: true
      events:
        method: GET
        path: /events
        stream: true
```

Path params like `{id}` are escaped as single path segments, including slashes and URL delimiters.
See [path parameter rules](docs/registry.md#path-parameters) for accepted values and validation errors.

| Setting under `upstream` | Default | Meaning |
|-------------------------|---------|---------|
| `request_timeout` | `30s` | Context deadline across dispatch, retries, and response reading |
| `dial_timeout` | `5s` | Connection establishment timeout |
| `keep_alive` | `30s` | TCP keepalive interval |
| `tls_handshake_timeout` | `5s` | TLS handshake timeout |
| `response_header_timeout` | `30s` | Wait for response headers |
| `idle_conn_timeout` | `90s` | Idle connection lifetime |
| `max_idle_conns` | `256` | Idle connections per service |
| `max_idle_conns_per_host` | `32` | Idle connections per upstream host |
| `max_conns_per_host` | `0` | Total connections per host. `0` means unlimited. |
| `max_response_bytes` | `1048576` | Buffered response limit. Range: 1024 to 67108864 bytes. |
| `max_stream_bytes` | `67108864` | SSE response limit. Range: 1024 to 1073741824 bytes. |
| `stream_idle_timeout` | `60s` | Maximum wait between SSE event boundaries |
| `retry.max_retries` | `2` | Additional attempts. `0` disables retries. |
| `retry.base_delay` | `100ms` | Initial exponential backoff ceiling |
| `retry.max_delay` | `2s` | Backoff and Retry-After cap |
| `breaker.failure_threshold` | `5` | Consecutive failed HTTP attempts before opening |
| `breaker.cooldown` | `30s` | Time before allowing 1 half-open probe |

Duration values use Go syntax, such as `100ms` or `10s`.
Zero values use defaults, except `max_conns_per_host` and `retry.max_retries`.
Negative values fail registry loading. The retry base delay must not exceed its cap.
HTTP/2 is enabled. Proxy selection follows `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`.
Redirects must keep the original scheme and host, including its port. HTTPS downgrades are refused.
Same-origin redirects allow at most 5 hops. Custom client policies can further restrict redirects, but cannot bypass these checks.
Origin and hop-limit rejections return a fixed upstream error and still commit 1 receipt.
They never retry or count as breaker failures.
An earlier caller deadline wins. Embedded `Config.HTTPClient.Timeout` does not replace the context deadline.

GET, HEAD, and OPTIONS can retry transport errors and HTTP 502, 503, or 504.
PUT and DELETE need `idempotent: true` on their registry action.
POST, PATCH, and other methods never retry, even with that flag.
Bodies must be replayable through `http.Request.GetBody`. Gateway JSON bodies already support this.
Each retry uses full-jitter exponential backoff. Retry-After can increase the wait, but never beyond `retry.max_delay`.
Canceled requests stop retrying. Discarded response bodies close before waiting.

The gateway shares 1 retry token bucket across all service clients.
Its refill rate is 20% of the recent request rate, with a floor of 10 retries/s.
The rate uses a 1s sliding window, split into 100ms buckets.
Capacity equals 1s of refill tokens. The bucket starts with 10 tokens.
A separate window ceiling caps retries at 20% of requests, or 10 retries when traffic is low.
Retries do not add request credits. An empty bucket returns the last response or transport failure without retrying.

Transport errors and all HTTP 5xx responses count as breaker failures.
Caller cancellations and caller deadlines do not change the failure count. The pool's own request timeout still counts.
Other responses reset the consecutive failure count, including HTTP 4xx.
An open breaker fails fast with HTTP 503 and code `upstream_unavailable`.
After cooldown, only 1 probe runs. Success closes the breaker. Failure starts another cooldown.
A caller-canceled probe releases its permit without restarting cooldown. The next caller can probe immediately.

Internal retries still produce exactly 1 receipt per action attempt, containing the final status.
Breaker rejections also produce receipts, with `policyDecision: allow` and `errorCode: upstream_unavailable`.
Receipt encoding is unchanged. Transport failures return a fixed message without upstream error text.
Server logs contain structured failure diagnostics, never request headers or raw error strings.

### Response limits and SSE

Buffered responses never truncate silently. The gateway reads at most `max_response_bytes + 1` bytes.
Oversize responses return HTTP 502 with code `upstream_response_too_large`.
The fixed message is `upstream response exceeds configured limit`.
The receipt records status 502 and that error code. The upstream body closes without an unbounded drain.
JSON responses keep the `body` field. Other responses keep `body_text`.

SSE pass-through requires an action with `stream: true` and an agent request accepting `text/event-stream`.
The upstream must return HTTP 2xx with `Content-Type: text/event-stream`.
Otherwise, the normal buffered path applies, including `max_response_bytes`.
Successful streams return HTTP 200 without an `ActResponse` envelope.
The gateway commits 1 receipt before sending headers or stream bytes.
That receipt records status 200 and policy `allow`. It attests upstream acceptance, not stream completion.
Receipt failure returns HTTP 500 with `receipt_failed`, without exposing stream bytes.

`request_timeout` bounds streaming dispatch and retries. Its deadline lifts after qualifying upstream headers.
Caller deadlines still apply. Streams use `max_stream_bytes` and `stream_idle_timeout` instead of the buffered body limit.
Blank-line event boundaries reset the idle timeout. Partial event bytes don't reset it.
Limit breaches terminate the stream and close upstream. HTTP status cannot change after streaming starts.
Client disconnects also close upstream.
Server logs record the final SHA-256 digest, byte count, and termination reason.
The digest covers bytes accepted by downstream writes. It isn't part of the signed receipt.

See [registry response settings](docs/registry.md) and the [v2 stream digest proposal](docs/spec/proposals/receipt-stream-digest.md).

## Tech Stack

- **Go 1.22+** — single binary, no runtime dependencies
- **SQLite** — embedded database for keys, tokens, audit log
- **AES-256-GCM** — token encryption at rest
- **HMAC-SHA256** for API key verification. bcrypt remains for legacy keys.
- **Docker** — containerized deployment

## Development

```bash
# Build
make build

# Run tests
make test

# Run locally
make run

# Lint
make lint
```

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for
prerequisites, build/test/lint instructions, focused pull request
guidance, and DCO sign-off. Issues labeled [`good first
issue`](https://github.com/Clawdlinux/agentgate/labels/good%20first%20issue)
are scoped to be independently testable without needing secrets or
maintainer context.

## License

AgentGate is licensed under the Apache License 2.0. See [LICENSE](LICENSE).
