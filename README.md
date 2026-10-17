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

```
Agent → POST /v1/act → [Auth MW] → [Registry] → [Vault: get token] → [Proxy: call upstream] → Response
```

```
┌─────────────────────────────────────────────────────┐
│                    AI AGENT                          │
│  POST http://localhost:8080/v1/act                   │
│  { service, action, on_behalf_of, params }           │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│                 AGENTGATE GATEWAY                    │
│                                                      │
│  Auth MW → Registry → Vault → Proxy → Upstream       │
│  Rate Limiter │ Audit Logger │ OAuth Callbacks        │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────┐
│               UPSTREAM SaaS APIs                     │
│      GitHub  │  Slack  │  Google Workspace           │
└─────────────────────────────────────────────────────┘
```

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
5. **Rate limiting.** Per-(agent, service) token bucket prevents runaway API usage.
6. **OAuth state encrypted** with AES-256-GCM and expires after 10 minutes.
7. **OAuth refresh.** Configured OAuth providers refresh tokens expiring within 5 minutes before dispatch. A failed refresh returns `token_expired` without calling the SaaS API.

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `AGENTGATE_VAULT_KEY` | 32-byte vault key. Also derives receipt encryption keys and the API-key HMAC pepper. | Yes |
| `AGENTGATE_ADMIN_SECRET` | Secret for admin API access | Yes |
| `AGENTGATE_PUBLIC_URL` | Base URL used to build the OAuth callback (default `http://localhost:8080`) | No |
| `AGENTGATE_API_KEY_CACHE_TTL` | Verified-key cache duration. Default `30s`; `0` disables caching. | No |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | GitHub OAuth credentials | For GitHub OAuth |
| `SLACK_CLIENT_ID` / `SLACK_CLIENT_SECRET` | Slack OAuth credentials | For Slack OAuth |
| `GOOGLE_WORKSPACE_CLIENT_ID` / `GOOGLE_WORKSPACE_CLIENT_SECRET` | Google OAuth credentials, requesting only the narrow `gmail.labels` scope | For Google Workspace OAuth |
| `STRIPE_CLIENT_ID` / `STRIPE_CLIENT_SECRET` | Stripe OAuth credentials (Stripe remains configured and functional, just not a featured launch connector) | For Stripe OAuth |

Agent API keys are bootstrapped on first boot and logged once. There is no environment variable for a pre-supplied key.
SQLite stores a public ID and a peppered HMAC-SHA256, never the secret. Use `POST /admin/keys` to create more.
Legacy `ag_live_` rows keep their bcrypt hashes. Successful validation adds an indexed HMAC without changing the presented key.
Revoked rows never enter legacy bcrypt scans.

### Service Configs

Service configurations are YAML files in `configs/services/`. See `configs/services/google_workspace.yaml` for an example. The gateway itself loads the merged `configs/services.yaml`.

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
