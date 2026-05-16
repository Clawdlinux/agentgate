# AgentGate — Architecture Design Document

## 1. Product Summary

**AgentGate** is a thin API gateway that lets AI agents authenticate to and call SaaS APIs on behalf of humans. Agents never see tokens. The gateway handles OAuth token exchange, refresh, and injection transparently.

**Repo:** `~/clawdlinux/agentgate`  
**Language:** Go  
**Deploy (MVP):** Docker Compose (local)  
**Integrations (MVP):** Stripe, GitHub, Slack  

---

## 2. System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         AGENT (any language)                      │
│  POST http://localhost:8080/v1/act                                │
│  {                                                                │
│    "service": "stripe",                                           │
│    "action": "GET /v1/invoices",                                  │
│    "on_behalf_of": "user-42",                                     │
│    "params": {"limit": 10}                                        │
│  }                                                                │
└────────────────────────────────┬────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                        AGENTGATE GATEWAY                          │
│                                                                   │
│  ┌──────────┐  ┌──────────────┐  ┌─────────────┐  ┌──────────┐ │
│  │ Auth     │  │ Service      │  │ Token       │  │ Request  │ │
│  │ Middleware│  │ Router       │  │ Vault       │  │ Builder  │ │
│  │ (API key │  │ (OpenAPI     │  │ (encrypted  │  │ (construct│ │
│  │  + agent │  │  spec match) │  │  store,     │  │  upstream │ │
│  │  identity)│  │              │  │  auto-      │  │  HTTP    │ │
│  │          │  │              │  │  refresh)   │  │  request)│ │
│  └──────────┘  └──────────────┘  └─────────────┘  └──────────┘ │
│                                                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐    │
│  │ Rate Limiter │  │ Audit Logger │  │ Webhook Receiver    │    │
│  │ (per-agent,  │  │ (every call  │  │ (OAuth callbacks)   │    │
│  │  per-service)│  │  logged)     │  │                     │    │
│  └──────────────┘  └──────────────┘  └─────────────────────┘    │
└────────────────────────────────┬────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────┐
│                     UPSTREAM SAAS APIs                            │
│         Stripe API    │    GitHub API    │    Slack API           │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Core Components

### 3.1 Gateway Server (`cmd/agentgate/main.go`)
- HTTP server (net/http or chi router)
- Single endpoint: `POST /v1/act`
- Health check: `GET /healthz`
- OAuth callback: `GET /auth/callback/{service}`
- Admin UI: `GET /admin/*` (optional, Phase 2)

### 3.2 Auth Middleware (`internal/auth/`)
- Agent authenticates with an API key (header: `X-AgentGate-Key`)
- API keys are scoped: which services, which users, what actions
- Keys stored in SQLite (MVP) with bcrypt hashes

### 3.3 Service Router (`internal/router/`)
- Maps `service` + `action` to upstream URL + method
- Each service defined by a config file (not full OpenAPI — simplified):
```yaml
# configs/services/stripe.yaml
name: stripe
base_url: https://api.stripe.com
auth_type: bearer
token_header: Authorization
token_prefix: "Bearer "
endpoints:
  - action: "GET /v1/invoices"
    upstream: "GET /v1/invoices"
    scopes: ["read_invoices"]
  - action: "POST /v1/invoices"
    upstream: "POST /v1/invoices"
    scopes: ["write_invoices"]
```

### 3.4 Token Vault (`internal/vault/`)
- Stores OAuth tokens encrypted at rest (AES-256-GCM)
- Schema: `(user_id, service, access_token, refresh_token, expires_at)`
- Auto-refresh: if token expired, refresh before proxying
- Storage backend: SQLite (MVP), PostgreSQL (production)
- Encryption key from env var `AGENTGATE_VAULT_KEY`

### 3.5 Request Builder (`internal/proxy/`)
- Constructs upstream HTTP request
- Injects auth token from vault
- Forwards agent's params as query/body depending on method
- Returns upstream response to agent (passthrough, no transformation)

### 3.6 Rate Limiter (`internal/ratelimit/`)
- Token bucket per (agent_key, service)
- Configurable per service (Stripe: 100 req/s, GitHub: 5000 req/h)
- Returns 429 with `Retry-After` header

### 3.7 Audit Logger (`internal/audit/`)
- Every proxied request logged: timestamp, agent_key, service, action, user_id, status_code, latency_ms
- SQLite table (MVP), structured JSON logs to stdout

---

## 4. Data Model (SQLite)

```sql
-- Agent API keys
CREATE TABLE agent_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    allowed_services TEXT NOT NULL, -- JSON array
    allowed_users TEXT NOT NULL,    -- JSON array or "*"
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    revoked_at DATETIME
);

-- OAuth tokens (encrypted)
CREATE TABLE tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    service TEXT NOT NULL,
    access_token_enc BLOB NOT NULL,
    refresh_token_enc BLOB NOT NULL,
    expires_at DATETIME NOT NULL,
    scopes TEXT NOT NULL, -- JSON array
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, service)
);

-- Audit log
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    agent_key_id TEXT NOT NULL,
    service TEXT NOT NULL,
    action TEXT NOT NULL,
    user_id TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    error TEXT
);
```

---

## 5. OAuth Flow (User Linking)

```
1. Developer calls: POST /admin/link
   { "user_id": "user-42", "service": "stripe" }

2. Gateway returns OAuth authorize URL:
   https://connect.stripe.com/oauth/authorize?
     client_id=...&redirect_uri=http://localhost:8080/auth/callback/stripe
     &state=<encrypted(user_id)>

3. User clicks link, authorizes in browser

4. Stripe redirects to: GET /auth/callback/stripe?code=...&state=...

5. Gateway exchanges code for tokens, encrypts, stores in vault

6. Done. Agent can now act on behalf of user-42 on Stripe.
```

---

## 6. Request Flow (Agent → SaaS)

```
1. Agent sends:
   POST /v1/act
   X-AgentGate-Key: ag_live_abc123
   {
     "service": "github",
     "action": "GET /repos/{owner}/{repo}/issues",
     "on_behalf_of": "user-42",
     "params": {"owner": "clawdlinux", "repo": "agentgate", "state": "open"}
   }

2. Auth middleware validates API key → resolves agent identity + permissions

3. Service router matches "github" + action → upstream URL template

4. Token vault fetches user-42's GitHub token (refreshes if expired)

5. Request builder constructs:
   GET https://api.github.com/repos/clawdlinux/agentgate/issues?state=open
   Authorization: Bearer ghp_xxx...
   User-Agent: AgentGate/0.1.0

6. Proxy executes request, gets response

7. Audit logger records the call

8. Response returned to agent (passthrough JSON)
```

---

## 7. Service Configs (MVP)

### Stripe
- OAuth: Connect standard
- Base URL: `https://api.stripe.com`
- Auth: Bearer token
- Key actions: list invoices, create charges, list customers

### GitHub
- OAuth: GitHub Apps (user-to-server tokens)
- Base URL: `https://api.github.com`
- Auth: Bearer token
- Key actions: list repos, list issues, create issue, list PRs

### Slack
- OAuth: Slack OAuth V2 (Bot + User tokens)
- Base URL: `https://slack.com/api`
- Auth: Bearer token
- Key actions: post message, list channels, list users

---

## 8. Directory Structure

```
agentgate/
├── cmd/
│   └── agentgate/
│       └── main.go              # Entry point
├── internal/
│   ├── auth/
│   │   ├── middleware.go        # API key validation
│   │   └── keys.go             # Key management
│   ├── router/
│   │   ├── router.go           # Service + action routing
│   │   └── loader.go           # Load service configs
│   ├── vault/
│   │   ├── vault.go            # Token CRUD + encryption
│   │   ├── refresh.go          # Auto-refresh logic
│   │   └── crypto.go           # AES-256-GCM helpers
│   ├── proxy/
│   │   ├── handler.go          # /v1/act handler
│   │   └── builder.go          # Construct upstream request
│   ├── oauth/
│   │   ├── handler.go          # /auth/callback handler
│   │   ├── providers.go        # Provider-specific OAuth configs
│   │   └── state.go            # State param encryption
│   ├── ratelimit/
│   │   └── limiter.go          # Token bucket implementation
│   ├── audit/
│   │   └── logger.go           # Audit log writer
│   ├── admin/
│   │   └── handler.go          # /admin/* endpoints (link accounts)
│   └── db/
│       ├── sqlite.go           # SQLite connection + migrations
│       └── migrations/
│           └── 001_init.sql
├── configs/
│   ├── services/
│   │   ├── stripe.yaml
│   │   ├── github.yaml
│   │   └── slack.yaml
│   └── agentgate.yaml          # Main config (port, vault key ref, etc.)
├── pkg/
│   └── sdk/
│       └── client.go           # Go SDK for agents to call agentgate
├── docker-compose.yaml
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── .github/
    └── copilot-instructions.md  # Copilot agent context
```

---

## 9. Config File (`configs/agentgate.yaml`)

```yaml
server:
  port: 8080
  host: "0.0.0.0"

vault:
  encryption_key_env: "AGENTGATE_VAULT_KEY"
  backend: "sqlite"

database:
  driver: "sqlite3"
  dsn: "./data/agentgate.db"

oauth:
  callback_base_url: "http://localhost:8080"
  providers:
    stripe:
      client_id_env: "STRIPE_CLIENT_ID"
      client_secret_env: "STRIPE_CLIENT_SECRET"
      authorize_url: "https://connect.stripe.com/oauth/authorize"
      token_url: "https://connect.stripe.com/oauth/token"
      scopes: ["read_write"]
    github:
      client_id_env: "GITHUB_CLIENT_ID"
      client_secret_env: "GITHUB_CLIENT_SECRET"
      authorize_url: "https://github.com/login/oauth/authorize"
      token_url: "https://github.com/login/oauth/access_token"
      scopes: ["repo", "read:org"]
    slack:
      client_id_env: "SLACK_CLIENT_ID"
      client_secret_env: "SLACK_CLIENT_SECRET"
      authorize_url: "https://slack.com/oauth/v2/authorize"
      token_url: "https://slack.com/api/oauth.v2.access"
      scopes: ["chat:write", "channels:read", "users:read"]

rate_limits:
  stripe:
    requests_per_second: 25
  github:
    requests_per_hour: 5000
  slack:
    requests_per_minute: 60
```

---

## 10. Docker Setup

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agentgate ./cmd/agentgate

FROM alpine:3.19
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /agentgate /usr/local/bin/agentgate
COPY configs/ /etc/agentgate/configs/
EXPOSE 8080
ENTRYPOINT ["agentgate"]
```

```yaml
# docker-compose.yaml
version: "3.9"
services:
  agentgate:
    build: .
    ports:
      - "8080:8080"
    environment:
      - AGENTGATE_VAULT_KEY=dev-key-change-in-production-32bytes!
      - STRIPE_CLIENT_ID=${STRIPE_CLIENT_ID}
      - STRIPE_CLIENT_SECRET=${STRIPE_CLIENT_SECRET}
      - GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID}
      - GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET}
      - SLACK_CLIENT_ID=${SLACK_CLIENT_ID}
      - SLACK_CLIENT_SECRET=${SLACK_CLIENT_SECRET}
    volumes:
      - ./data:/data
      - ./configs:/etc/agentgate/configs
```

---

## 11. API Contract

### POST /v1/act
**Request:**
```json
{
  "service": "stripe",
  "action": "GET /v1/invoices",
  "on_behalf_of": "user-42",
  "params": {
    "limit": 10,
    "status": "open"
  }
}
```

**Response (success):**
```json
{
  "status": 200,
  "service": "stripe",
  "action": "GET /v1/invoices",
  "data": { /* raw upstream response */ },
  "meta": {
    "latency_ms": 142,
    "cached": false
  }
}
```

**Response (error):**
```json
{
  "status": 401,
  "error": "token_expired_refresh_failed",
  "message": "OAuth token for user-42 on stripe has expired and refresh failed. Re-link required.",
  "relink_url": "http://localhost:8080/admin/link?user_id=user-42&service=stripe"
}
```

### POST /admin/link
```json
{
  "user_id": "user-42",
  "service": "stripe"
}
```
→ Returns `{ "authorize_url": "https://..." }`

### POST /admin/keys
```json
{
  "name": "my-agent",
  "allowed_services": ["stripe", "github"],
  "allowed_users": ["user-42", "user-99"]
}
```
→ Returns `{ "key": "ag_live_..." }` (shown once)

---

## 12. Security Model

1. **Agent keys** are scoped (service × user). An agent can only access what it's explicitly granted.
2. **Tokens encrypted at rest.** AES-256-GCM. Key never in DB.
3. **No token exposure.** Agents never see OAuth tokens — only the gateway touches them.
4. **Audit trail.** Every call logged with full context.
5. **Rate limiting.** Prevents runaway agents from burning API quotas.
6. **State param encrypted** in OAuth flow to prevent CSRF.

---

## 13. Success Criteria (MVP)

- [ ] `docker-compose up` starts the gateway
- [ ] Can create an API key via `/admin/keys`
- [ ] Can link a user to Stripe/GitHub/Slack via OAuth
- [ ] Agent can call `POST /v1/act` and get proxied response
- [ ] Token auto-refresh works when token expires
- [ ] Rate limiting returns 429 appropriately
- [ ] Audit log captures all calls
- [ ] Go SDK client works in a test script
