# AgentGate

A thin API gateway that lets AI agents call SaaS APIs (Stripe, GitHub, Slack) on behalf of users. Agents never see tokens — the gateway handles OAuth, encrypted token storage, and request proxying.

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

## Quick Start

```bash
# Clone
git clone https://github.com/clawdlinux/agentgate.git
cd agentgate

# Configure
cp .env.example .env
# Edit .env with your keys

# Run with Docker
docker-compose up --build

# Or run directly
export AGENT_API_KEY=ag_dev_test_key
go run ./cmd/agentgw --addr :8080

# Health check
curl http://localhost:8080/healthz
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
4. **Audit trail.** Every proxied call logged with agent, user, service, action, status, and latency.
5. **Rate limiting.** Per-(agent, service) token bucket prevents runaway API usage.
6. **OAuth state encrypted** with AES-256-GCM and expires after 10 minutes.

## Configuration

### Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `AGENTGATE_VAULT_KEY` | 32-byte encryption key for token vault | Yes |
| `AGENTGATE_ADMIN_SECRET` | Secret for admin API access | Yes |
| `AGENT_API_KEY` | MVP: single agent API key | No |
| `VAULT_ENCRYPTION_KEY` | Alternative vault key (32 bytes) | No |
| `STRIPE_CLIENT_ID` | Stripe OAuth client ID | For OAuth |
| `STRIPE_CLIENT_SECRET` | Stripe OAuth client secret | For OAuth |
| `GITHUB_CLIENT_ID` | GitHub OAuth client ID | For OAuth |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth client secret | For OAuth |
| `SLACK_CLIENT_ID` | Slack OAuth client ID | For OAuth |
| `SLACK_CLIENT_SECRET` | Slack OAuth client secret | For OAuth |

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
