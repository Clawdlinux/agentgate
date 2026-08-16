# AgentGate

A thin API gateway that lets AI agents call SaaS APIs (GitHub, Slack, Stripe) on behalf of users. Agents never see tokens — the gateway handles OAuth, encrypted token storage, and request proxying. Every action gets a signed, gap-free receipt that anyone can verify offline, without AgentGate's secret key.

## Quickstart

Five steps, no host Go toolchain required — Docker builds and runs everything.

```bash
git clone https://github.com/Clawdlinux/agentgate.git
cd agentgate
docker compose up -d --build
```

The container bootstraps one agent API key on first boot and logs it once:

```bash
docker compose logs agentgate | grep agent_key
# {"agent_key":"ag_live_..."} — save this, it is never shown again
```

**Connect GitHub.** Register a GitHub OAuth App once at
[github.com/settings/developers](https://github.com/settings/developers)
(callback URL `http://localhost:8080/auth/callback/github`), then set
`GITHUB_CLIENT_ID`/`GITHUB_CLIENT_SECRET` in a `.env` file (see
`.env.example`) before `docker compose up`. Get the authorization link and
open it in a browser to grant access:

```bash
curl -s -X POST http://localhost:8080/admin/link \
  -H "X-Admin-Secret: admin-dev-secret-change-me!!" \
  -H "Content-Type: application/json" \
  -d '{"user_id":"demo-user","service":"github"}'
# open the returned authorize_url, click Authorize
```

**Call an action** and it returns a real response:

```bash
curl -s -X POST http://localhost:8080/v1/act \
  -H "Authorization: Bearer <agent-api-key-from-above>" \
  -H "Content-Type: application/json" \
  -d '{"service":"github","action":"list_repos","on_behalf_of":"demo-user","params":{"per_page":1}}'
```

**Verify its receipt** — run this inside the container so it reads the
same view of the database the gateway just wrote, avoiding a bind-mount
read lag some Docker Desktop setups have when reading a live SQLite file
from the host:

```bash
docker compose exec agentgate sh -c '
  wget -qO- http://localhost:8080/v1/receipts/pubkey > /tmp/trust.json
  agentgate-verify --source sqlite --path /data/agentgate.db --trust-root /tmp/trust.json
'
# PASS: 1 receipts verified, head seq=1 hash=...
```

Stop and restart the container (`docker compose restart agentgate`) and
run the verify command again — the signing identity and the receipt are
still there, from the bind-mounted `./data/agentgate.db`.

To verify from the host instead, stop the container first (a running
container's WAL writes can lag behind what a separate host process sees
on Docker Desktop's virtual filesystem) or copy the file out; then run
`make build-verify && ./bin/agentgate-verify --source sqlite --path ./data/agentgate.db --trust-root trust.json`.

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
│       Stripe  │  GitHub  │  Slack                    │
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
{"name": "my-agent", "allowed_services": ["stripe", "github"], "allowed_users": ["user-42"]}
```

#### DELETE /admin/keys/{id}
Revoke an API key.

#### POST /admin/link
Get OAuth authorization URL for user account linking.
```json
{"user_id": "user-42", "service": "github"}
```

#### GET /admin/tokens/{user_id}
List linked services for a user (no token values exposed).

### OAuth Callback

#### GET /auth/callback/{service}
OAuth redirect handler — exchanges code for tokens, stores encrypted in vault.

## Go SDK

```go
import "github.com/Clawdlinux/agentgate/pkg/sdk"

client := sdk.NewClient("http://localhost:8080", "ag_live_...")

// Call any service
resp, err := client.Act(ctx, sdk.ActRequest{
    Service:    "github",
    Action:     "list_repos",
    OnBehalfOf: "user-42",
})

// Convenience helpers
resp, err := client.Stripe(ctx, "user-42", "list_invoices", map[string]interface{}{"limit": 10})
resp, err := client.GitHub(ctx, "user-42", "list_repos", nil)
resp, err := client.Slack(ctx, "user-42", "post_message", map[string]interface{}{"channel": "#general", "text": "Hello"})

// Error handling
if sdk.IsTokenMissing(err) {
    // User needs to link their account
}
if sdk.IsRateLimited(err) {
    // Back off and retry
}
```

## Security Model

1. **Agent keys** are scoped (service × user). Agents can only access what's explicitly granted.
2. **Tokens encrypted at rest.** AES-256-GCM with 32-byte key from environment.
3. **No token exposure.** Agents never see OAuth tokens — only the gateway touches them.
4. **Signed receipts.** Every authenticated action attempt commits one Ed25519-signed, hash-chained receipt before the response is returned — verify offline with `agentgate-verify`, no gateway state or private key needed.
5. **Rate limiting.** Per-(agent, service) token bucket prevents runaway API usage.
6. **OAuth state encrypted** with AES-256-GCM and expires after 10 minutes.

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `AGENTGATE_VAULT_KEY` | 32-byte encryption key for the token vault and the receipt signing key | Yes |
| `AGENTGATE_ADMIN_SECRET` | Secret for admin API access | Yes |
| `AGENTGATE_PUBLIC_URL` | Base URL used to build the OAuth callback (default `http://localhost:8080`) | No |
| `STRIPE_CLIENT_ID` / `STRIPE_CLIENT_SECRET` | Stripe OAuth credentials | For Stripe OAuth |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | GitHub OAuth credentials | For GitHub OAuth |
| `SLACK_CLIENT_ID` / `SLACK_CLIENT_SECRET` | Slack OAuth credentials | For Slack OAuth |

Agent API keys are bootstrapped automatically on first boot and logged once — there is no env var for a pre-supplied key (they are bcrypt-hashed in SQLite; see `POST /admin/keys` to create more).

### Service Configs

Service configurations are YAML files in `configs/services/`. See `configs/services/github.yaml` for an example.

## Tech Stack

- **Go 1.22+** — single binary, no runtime dependencies
- **SQLite** — embedded database for keys, tokens, audit log
- **AES-256-GCM** — token encryption at rest
- **bcrypt** — API key hashing
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

## License

AgentGate is licensed under the Apache License 2.0. See [LICENSE](LICENSE).
