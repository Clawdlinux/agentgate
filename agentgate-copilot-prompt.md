# AgentGate — GitHub Copilot Agent Mode Overnight Prompt

## How to Use

1. Open VS Code in `~/clawdlinux/agentgate` (empty repo, just `git init`)
2. Open Copilot Chat → Switch to **Agent mode** (@workspace)
3. Paste the entire prompt below (everything between the `---` markers)
4. Let it run. It will execute all 7 phases sequentially.

If it stalls or hits context limits, paste the "Resume Prompt" at the bottom.

---

## FULL PROMPT (copy everything below this line)

---

You are building `agentgate` — a production-quality Go API gateway that lets AI agents call SaaS APIs (Stripe, GitHub, Slack) on behalf of users without ever seeing OAuth tokens. The gateway handles token storage, encryption, auto-refresh, and request proxying.

**CRITICAL RULES:**
- Execute phases IN ORDER (1→7). Do not skip ahead.
- After each phase, run `go build ./cmd/agentgate` AND `go test ./...`
- If either fails, FIX THE ISSUE before proceeding to the next phase.
- Commit after each phase with the specified commit message.
- Use Go 1.22+, chi router, SQLite (CGO enabled), zerolog for logging.
- Follow standard Go project layout. No global state. Dependency injection.
- Table-driven tests. Context propagation. Errors wrapped with `%w`.

---

### PHASE 1: PROJECT SCAFFOLD

Initialize the Go module and create the full directory structure.

```bash
go mod init github.com/clawdlinux/agentgate
```

**Dependencies (go get):**
- github.com/go-chi/chi/v5
- github.com/mattn/go-sqlite3
- gopkg.in/yaml.v3
- golang.org/x/crypto
- github.com/rs/zerolog
- golang.org/x/time/rate

**Create these files:**

`cmd/agentgate/main.go` — Entry point. Loads config, opens DB, runs migrations, sets up chi router with placeholder handlers, starts HTTP server with graceful shutdown (os.Signal).

`configs/agentgate.yaml`:
```yaml
server:
  port: 8080
  host: "0.0.0.0"
vault:
  encryption_key_env: "AGENTGATE_VAULT_KEY"
database:
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
admin:
  secret_env: "AGENTGATE_ADMIN_SECRET"
```

`internal/db/sqlite.go` — OpenDB(dsn) and RunMigrations(db) functions.

`internal/db/migrations/001_init.sql`:
```sql
CREATE TABLE IF NOT EXISTS agent_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    allowed_services TEXT NOT NULL DEFAULT '["*"]',
    allowed_users TEXT NOT NULL DEFAULT '["*"]',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    revoked_at DATETIME
);

CREATE TABLE IF NOT EXISTS tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    service TEXT NOT NULL,
    access_token_enc BLOB NOT NULL,
    refresh_token_enc BLOB NOT NULL,
    expires_at DATETIME NOT NULL,
    scopes TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, service)
);

CREATE TABLE IF NOT EXISTS audit_log (
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

CREATE INDEX IF NOT EXISTS idx_tokens_user_service ON tokens(user_id, service);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit_log(agent_key_id);
```

`internal/config/config.go` — Struct matching the YAML + Load(path) function.

`Makefile`:
```makefile
.PHONY: build run test docker lint clean

build:
	CGO_ENABLED=1 go build -o bin/agentgate ./cmd/agentgate

run: build
	./bin/agentgate

test:
	CGO_ENABLED=1 go test -v ./...

docker:
	docker-compose up --build

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ data/
```

`Dockerfile`:
```dockerfile
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agentgate ./cmd/agentgate

FROM alpine:3.19
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /agentgate /usr/local/bin/agentgate
COPY configs/ /etc/agentgate/configs/
RUN mkdir -p /data
ENV AGENTGATE_CONFIG=/etc/agentgate/configs/agentgate.yaml
EXPOSE 8080
ENTRYPOINT ["agentgate"]
```

`docker-compose.yaml`:
```yaml
version: "3.9"
services:
  agentgate:
    build: .
    ports:
      - "8080:8080"
    environment:
      - AGENTGATE_VAULT_KEY=00000000000000000000000000000000
      - AGENTGATE_ADMIN_SECRET=admin-dev-secret
      - STRIPE_CLIENT_ID=placeholder
      - STRIPE_CLIENT_SECRET=placeholder
      - GITHUB_CLIENT_ID=placeholder
      - GITHUB_CLIENT_SECRET=placeholder
      - SLACK_CLIENT_ID=placeholder
      - SLACK_CLIENT_SECRET=placeholder
    volumes:
      - ./data:/data
      - ./configs:/etc/agentgate/configs
```

**Verify:** `go mod tidy && go build ./cmd/agentgate` must succeed.  
**Commit:** `git add -A && git commit -m "feat: scaffold agentgate project structure"`

---

### PHASE 2: TOKEN VAULT (Encrypted Storage)

Create `internal/vault/` package.

`internal/vault/crypto.go`:
- `Encryptor` struct holding a 32-byte key
- `Encrypt(plaintext []byte) ([]byte, error)` — AES-256-GCM, random 12-byte nonce prepended to ciphertext
- `Decrypt(ciphertext []byte) ([]byte, error)` — extract nonce, decrypt
- Validate key is exactly 32 bytes in constructor

`internal/vault/vault.go`:
- `Token` struct: UserID, Service, AccessToken, RefreshToken, ExpiresAt, Scopes
- `Vault` struct: db + Encryptor
- `NewVault(db, key) (*Vault, error)`
- `Store(ctx, Token) error` — encrypt tokens, upsert (INSERT OR REPLACE)
- `Get(ctx, userID, service) (*Token, error)` — decrypt on read
- `Delete(ctx, userID, service) error`
- `ListForUser(ctx, userID) ([]Token, error)` — returns services linked (tokens decrypted)

`internal/vault/refresh.go`:
- `RefreshFunc = func(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, expiresIn time.Duration, err error)`
- `GetOrRefresh(ctx, userID, service, RefreshFunc) (*Token, error)`:
  - Get token from DB
  - If expires within 5 min → call RefreshFunc → update DB
  - If refresh fails → return error with context
  - Return valid token

**Tests (use in-memory SQLite `:memory:`):**
- `crypto_test.go`: encrypt/decrypt round-trip, tampered ciphertext fails, wrong key fails
- `vault_test.go`: store + get, update existing, delete, list
- `refresh_test.go`: fresh token skips refresh, expired triggers refresh, failed refresh returns error

**Verify:** `go test ./internal/vault/...` passes.  
**Commit:** `git add -A && git commit -m "feat: implement encrypted token vault with auto-refresh"`

---

### PHASE 3: SERVICE ROUTER & REQUEST BUILDER

Create `internal/router/` and `internal/proxy/` packages.

`internal/router/loader.go`:
- `ServiceConfig` struct: Name, BaseURL, AuthType, TokenHeader, TokenPrefix, Endpoints[]
- `EndpointConfig` struct: Action, Upstream, Scopes[]
- `LoadServices(configDir string) (map[string]*ServiceConfig, error)` — glob `*.yaml`, parse each

`internal/router/router.go`:
- `Router` struct holding services map
- `Resolve(service, action string) (*ServiceConfig, *EndpointConfig, error)`

`internal/proxy/builder.go`:
- `BuildUpstreamRequest(svc *ServiceConfig, ep *EndpointConfig, token string, params map[string]interface{}) (*http.Request, error)`
- Parse method + path template from ep.Upstream
- Substitute `{param}` placeholders from params map
- GET → remaining params as query string
- POST/PUT/PATCH → remaining params as JSON body
- Set Authorization header per svc config

`internal/proxy/handler.go`:
- `ActHandler` struct with dependencies (router, vault, httpClient, auditLogger)
- `ServeHTTP` for `POST /v1/act`
- Decode request → resolve route → get token (vault.GetOrRefresh) → build upstream request → execute → write response
- Measure latency, log to audit

Create `configs/services/stripe.yaml`:
```yaml
name: stripe
base_url: https://api.stripe.com
auth_type: bearer
token_header: Authorization
token_prefix: "Bearer "
endpoints:
  - action: "GET /v1/invoices"
    upstream: "GET /v1/invoices"
    scopes: ["read_write"]
  - action: "GET /v1/customers"
    upstream: "GET /v1/customers"
    scopes: ["read_write"]
  - action: "POST /v1/charges"
    upstream: "POST /v1/charges"
    scopes: ["read_write"]
  - action: "GET /v1/balance"
    upstream: "GET /v1/balance"
    scopes: ["read_write"]
```

Create `configs/services/github.yaml`:
```yaml
name: github
base_url: https://api.github.com
auth_type: bearer
token_header: Authorization
token_prefix: "Bearer "
endpoints:
  - action: "GET /repos/{owner}/{repo}/issues"
    upstream: "GET /repos/{owner}/{repo}/issues"
    scopes: ["repo"]
  - action: "POST /repos/{owner}/{repo}/issues"
    upstream: "POST /repos/{owner}/{repo}/issues"
    scopes: ["repo"]
  - action: "GET /user/repos"
    upstream: "GET /user/repos"
    scopes: ["repo"]
  - action: "GET /repos/{owner}/{repo}/pulls"
    upstream: "GET /repos/{owner}/{repo}/pulls"
    scopes: ["repo"]
```

Create `configs/services/slack.yaml`:
```yaml
name: slack
base_url: https://slack.com/api
auth_type: bearer
token_header: Authorization
token_prefix: "Bearer "
endpoints:
  - action: "POST /chat.postMessage"
    upstream: "POST /chat.postMessage"
    scopes: ["chat:write"]
  - action: "GET /conversations.list"
    upstream: "GET /conversations.list"
    scopes: ["channels:read"]
  - action: "GET /users.list"
    upstream: "GET /users.list"
    scopes: ["users:read"]
  - action: "POST /reactions.add"
    upstream: "POST /reactions.add"
    scopes: ["reactions:write"]
```

**Tests:**
- `router/loader_test.go`: load from test fixtures
- `router/router_test.go`: resolve known, resolve unknown service → error, unknown action → error
- `proxy/builder_test.go`: path params substituted, GET query string, POST JSON body, auth header set

**Verify:** `go test ./internal/router/... ./internal/proxy/...` passes.  
**Commit:** `git add -A && git commit -m "feat: implement service router and request builder"`

---

### PHASE 4: AUTH & OAUTH

Create `internal/auth/`, `internal/oauth/`, `internal/admin/` packages.

`internal/auth/keys.go`:
- Generate API key: `"ag_live_" + 32 random hex chars`
- Hash with bcrypt (cost 10)
- `KeyStore` struct with db
- `Create(ctx, name, allowedServices, allowedUsers) (key *AgentKey, plaintext string, error)`
- `Validate(ctx, plaintext string) (*AgentKey, error)` — scan all non-revoked keys, bcrypt compare
- `Revoke(ctx, keyID string) error`

`internal/auth/middleware.go`:
- Chi middleware: extract `X-AgentGate-Key` header → validate → set in context → or return 401 JSON
- `AgentKeyFromContext(ctx) *AgentKey` helper
- Check allowed_services contains requested service
- Check allowed_users contains on_behalf_of (or "*")

`internal/oauth/providers.go`:
- Load providers from config (resolve env vars for client_id/secret)
- `GetProvider(service string) (*Provider, error)`

`internal/oauth/state.go`:
- Encrypt: JSON marshal {user_id, service, exp} → AES-GCM encrypt → base64url
- Decrypt: reverse
- Expiry: 10 minutes

`internal/oauth/handler.go`:
- `GET /auth/callback/{service}`:
  - Extract code + state from query
  - Decrypt state → userID, service
  - Check not expired
  - Exchange code for tokens (HTTP POST to token_url)
  - Store in vault
  - Return simple HTML success page

`internal/admin/handler.go`:
- Admin auth: check `X-Admin-Secret` header matches env var
- `POST /admin/keys` → body: {name, allowed_services, allowed_users} → create key → return {id, key, name}
- `DELETE /admin/keys/{id}` → revoke
- `POST /admin/link` → body: {user_id, service} → build authorize URL with encrypted state → return {authorize_url}
- `GET /admin/tokens/{user_id}` → list linked services (no tokens exposed, just service names + expiry)

**Tests:**
- `auth/keys_test.go`: generate, validate correct, validate wrong, revoke
- `auth/middleware_test.go`: missing header→401, invalid key→401, valid→passes, revoked→401
- `oauth/state_test.go`: encrypt/decrypt roundtrip, expired state rejected

**Verify:** `go test ./internal/auth/... ./internal/oauth/... ./internal/admin/...` passes.  
**Commit:** `git add -A && git commit -m "feat: implement auth middleware, OAuth flow, admin endpoints"`

---

### PHASE 5: WIRE EVERYTHING + INTEGRATION TESTS

Update `cmd/agentgate/main.go` to wire all components together:
- Config → DB → Migrations → Vault → Router → KeyStore → Providers → Handlers → chi mux → Server

Add `internal/ratelimit/limiter.go`:
- Wraps `golang.org/x/time/rate`
- Per (agentKeyID, service) limiter
- Chi middleware that checks before proxying
- Returns 429 JSON with Retry-After

Add `internal/audit/logger.go`:
- Buffered channel (1000 entries)
- Background goroutine writes to DB
- `Log(entry)` is non-blocking
- Flush on shutdown

Create `test/integration_test.go`:
- Full end-to-end with real SQLite (temp dir)
- Start gateway with httptest.Server
- Mock upstream SaaS with separate httptest.Server (returns canned JSON)
- Override service config base_url to point at mock
- Tests:
  1. Health check returns 200
  2. Create API key via admin endpoint
  3. Store token directly in vault (skip OAuth for test)
  4. Call POST /v1/act → verify upstream called → verify response passed through
  5. Call without auth → 401
  6. Call with revoked key → 401
  7. Call unknown service → 400
  8. Rate limit burst → 429
  9. Verify audit log has entries

**Verify:** `go test ./...` passes. `go build ./cmd/agentgate` succeeds.  
**Commit:** `git add -A && git commit -m "feat: wire components, rate limiting, audit, integration tests"`

---

### PHASE 6: GO SDK

Create `pkg/sdk/` package.

`pkg/sdk/client.go`:
- `Client` struct: baseURL, apiKey, http.Client
- `NewClient(baseURL, apiKey, ...Option) *Client`
- Options: WithTimeout, WithHTTPClient

`pkg/sdk/act.go`:
- `ActRequest` and `ActResponse` structs
- `(c *Client) Act(ctx, ActRequest) (*ActResponse, error)`
- POST to /v1/act, set X-AgentGate-Key header
- Parse response, handle error responses

`pkg/sdk/helpers.go`:
- `(c *Client) Stripe(ctx, userID, action, params) (*ActResponse, error)`
- `(c *Client) GitHub(ctx, userID, action, params) (*ActResponse, error)`
- `(c *Client) Slack(ctx, userID, action, params) (*ActResponse, error)`

`pkg/sdk/errors.go`:
- `AgentGateError` with Status, Code, Message, RelinkURL
- Implements error interface
- `IsRelinkRequired(err) bool` helper

`examples/basic/main.go`:
```go
package main

import (
    "context"
    "fmt"
    "log"
    
    sdk "github.com/clawdlinux/agentgate/pkg/sdk"
)

func main() {
    client := sdk.NewClient("http://localhost:8080", "ag_live_your_key_here")
    
    resp, err := client.Stripe(context.Background(), "user-42", "GET /v1/invoices", map[string]interface{}{
        "limit": 10,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Status: %d\nData: %s\n", resp.Status, resp.Data)
}
```

**Tests:**
- `pkg/sdk/client_test.go`: mock server, success response, error response, relink error

**Verify:** `go test ./pkg/sdk/...` passes. `go build ./examples/basic` succeeds.  
**Commit:** `git add -A && git commit -m "feat: add Go SDK for agent consumers"`

---

### PHASE 7: QA & README

1. Run `go vet ./...` — fix all issues.
2. Run `go test ./...` — fix all failures.
3. Run `go build ./cmd/agentgate` — must succeed.
4. Verify `docker-compose up --build` starts (run and check logs for "listening on :8080").

Create `README.md`:
- Title: "AgentGate — API Gateway for AI Agents"
- One-paragraph description
- ASCII architecture diagram
- Quick start section (docker-compose up, create key, link account, make request)
- API reference for all endpoints
- SDK usage
- Security model
- Configuration reference
- License: MIT

Create `.env.example`:
```env
# Required
AGENTGATE_VAULT_KEY=generate-a-32-byte-hex-string-here
AGENTGATE_ADMIN_SECRET=your-admin-secret

# Stripe OAuth
STRIPE_CLIENT_ID=
STRIPE_CLIENT_SECRET=

# GitHub OAuth
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=

# Slack OAuth
SLACK_CLIENT_ID=
SLACK_CLIENT_SECRET=
```

Create `LICENSE` (MIT, copyright 2024 ClawdLinux).

**Commit:** `git add -A && git commit -m "docs: README, .env.example, LICENSE, final QA pass"`

---

## DONE. Final state: a fully working Go gateway with:
- Encrypted token vault with auto-refresh
- 3 SaaS integrations (Stripe, GitHub, Slack)  
- API key auth with scoping
- OAuth linking flow
- Rate limiting
- Audit logging
- Go SDK
- Docker deployment
- Comprehensive tests
- Full documentation

---

## Resume Prompt (if Copilot hits context limit)

```
Continue building agentgate. Check git log to see which phases are committed.
Run `go test ./...` to see current state. Pick up from the next uncommitted phase.
The phase instructions are in .github/copilot-instructions.md (check there).
Rules: fix failures before advancing, commit after each phase.
```
