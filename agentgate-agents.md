# AgentGate — Multi-Agent Build Orchestration

## Overview

This file defines a swarm of GitHub Copilot agents that work together to build the `agentgate` project overnight. Each agent owns a vertical slice and communicates via file artifacts. No human-in-the-loop required after kickoff.

**Repo:** `~/clawdlinux/agentgate`  
**Runtime:** Go 1.22+, SQLite, Docker  
**Architecture:** See `agentgate-architecture.md`

---

## Agent Roster

| Agent | Role | Owns | Depends On |
|-------|------|------|-----------|
| **Architect** | Scaffolds repo, go.mod, directory structure, configs | `cmd/`, `configs/`, `go.mod`, `Makefile`, `Dockerfile` | None (runs first) |
| **Vault Agent** | Token encryption, storage, auto-refresh | `internal/vault/`, `internal/db/` | Architect |
| **Router Agent** | Service config loading, action matching, request building | `internal/router/`, `internal/proxy/` | Architect |
| **Auth Agent** | API key middleware, OAuth callback flow, admin endpoints | `internal/auth/`, `internal/oauth/`, `internal/admin/` | Vault Agent |
| **Integration Agent** | Stripe, GitHub, Slack service configs + tests | `configs/services/`, integration tests | Router Agent, Auth Agent |
| **SDK Agent** | Go client SDK for agents to consume | `pkg/sdk/` | Router Agent |
| **QA Agent** | End-to-end tests, Docker verification, README | tests, `README.md`, `docker-compose.yaml` validation | All above |

---

## Phase 1: Architect Agent

**Trigger:** First. Runs immediately.  
**Skill:** `@workspace /new`

### Instructions

```
You are the Architect Agent for the agentgate project.

Create the complete project scaffold at ~/clawdlinux/agentgate:

1. Initialize Go module:
   go mod init github.com/clawdlinux/agentgate

2. Create directory structure exactly as specified in agentgate-architecture.md section 8.

3. Create go.mod with these dependencies:
   - github.com/go-chi/chi/v5 (router)
   - github.com/mattn/go-sqlite3 (database)
   - gopkg.in/yaml.v3 (config)
   - golang.org/x/crypto (bcrypt for key hashing)
   - github.com/rs/zerolog (structured logging)

4. Create Makefile with targets:
   - build: CGO_ENABLED=1 go build -o bin/agentgate ./cmd/agentgate
   - run: go run ./cmd/agentgate
   - test: go test ./...
   - docker: docker-compose up --build
   - migrate: run SQL migrations
   - lint: golangci-lint run

5. Create cmd/agentgate/main.go — skeleton that:
   - Loads config from configs/agentgate.yaml
   - Initializes SQLite DB + runs migrations
   - Sets up chi router with placeholder handlers
   - Starts HTTP server

6. Create configs/agentgate.yaml (copy from architecture doc section 9)

7. Create internal/db/migrations/001_init.sql (copy schema from architecture doc section 4)

8. Create internal/db/sqlite.go:
   - OpenDB(dsn string) (*sql.DB, error)
   - RunMigrations(db *sql.DB) error

9. Create Dockerfile and docker-compose.yaml (copy from architecture doc section 10)

10. Create .github/copilot-instructions.md with project context.

ACCEPTANCE CRITERIA:
- `go mod tidy` succeeds
- `go build ./cmd/agentgate` compiles (handlers can be stubs returning 501)
- Directory structure matches architecture doc exactly
- All config files are valid YAML

When done, commit: "feat: scaffold agentgate project structure"
```

---

## Phase 2: Vault Agent

**Trigger:** After Architect commits.  
**Skill:** `@workspace` focused on `internal/vault/` and `internal/db/`

### Instructions

```
You are the Vault Agent for agentgate. The project scaffold already exists.

Implement the Token Vault — encrypted OAuth token storage with auto-refresh.

1. internal/vault/crypto.go:
   - func NewEncryptor(key []byte) *Encryptor
   - func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) — AES-256-GCM
   - func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error)
   - Key must be exactly 32 bytes (from env var, error if wrong length)
   - Use crypto/aes + crypto/cipher, random nonce per encryption

2. internal/vault/vault.go:
   - type Token struct {
       UserID       string
       Service      string
       AccessToken  string
       RefreshToken string
       ExpiresAt    time.Time
       Scopes       []string
     }
   - type Vault struct { db *sql.DB; enc *Encryptor }
   - func NewVault(db *sql.DB, encryptionKey []byte) (*Vault, error)
   - func (v *Vault) Store(ctx context.Context, token Token) error
   - func (v *Vault) Get(ctx context.Context, userID, service string) (*Token, error)
   - func (v *Vault) Delete(ctx context.Context, userID, service string) error
   - func (v *Vault) ListForUser(ctx context.Context, userID string) ([]Token, error)
   - Encrypt access_token and refresh_token before INSERT
   - Decrypt on SELECT

3. internal/vault/refresh.go:
   - func (v *Vault) GetOrRefresh(ctx context.Context, userID, service string, refreshFn RefreshFunc) (*Token, error)
   - If token.ExpiresAt is within 5 minutes of now, call refreshFn
   - refreshFn signature: func(refreshToken string) (newAccess, newRefresh string, expiresIn time.Duration, err error)
   - Update stored token after refresh
   - If refresh fails, return error with "relink_required" context

4. Write unit tests:
   - internal/vault/crypto_test.go — round-trip encrypt/decrypt, wrong key fails
   - internal/vault/vault_test.go — store/get/delete with in-memory SQLite
   - internal/vault/refresh_test.go — expired token triggers refresh, fresh token skips

ACCEPTANCE CRITERIA:
- `go test ./internal/vault/...` passes
- Tokens are encrypted in DB (verified by raw SELECT showing gibberish)
- Auto-refresh logic works with mock refreshFn

When done, commit: "feat: implement encrypted token vault with auto-refresh"
```

---

## Phase 3: Router Agent

**Trigger:** After Architect commits.  
**Skill:** `@workspace` focused on `internal/router/` and `internal/proxy/`

### Instructions

```
You are the Router Agent for agentgate. The project scaffold already exists.

Implement service routing and upstream request building.

1. internal/router/loader.go:
   - type ServiceConfig struct {
       Name      string
       BaseURL   string
       AuthType  string // "bearer"
       TokenHeader string
       TokenPrefix string
       Endpoints []EndpointConfig
     }
   - type EndpointConfig struct {
       Action   string // "GET /v1/invoices"
       Upstream string // "GET /v1/invoices"
       Scopes   []string
     }
   - func LoadServices(configDir string) (map[string]*ServiceConfig, error)
   - Parse all YAML files in configs/services/

2. internal/router/router.go:
   - type Router struct { services map[string]*ServiceConfig }
   - func NewRouter(services map[string]*ServiceConfig) *Router
   - func (r *Router) Resolve(service, action string) (*ServiceConfig, *EndpointConfig, error)
   - Match by exact string: service name + action string
   - Return error if service not found or action not found

3. internal/proxy/builder.go:
   - type UpstreamRequest struct {
       Method  string
       URL     string
       Headers map[string]string
       Body    []byte
     }
   - func BuildRequest(svc *ServiceConfig, ep *EndpointConfig, token string, params map[string]interface{}) (*UpstreamRequest, error)
   - Parse action string for method + path
   - Substitute path params from params map (e.g., {owner} → params["owner"])
   - For GET: remaining params → query string
   - For POST/PUT/PATCH: remaining params → JSON body
   - Set auth header: TokenHeader: TokenPrefix + token

4. internal/proxy/handler.go:
   - type ActRequest struct {
       Service    string                 `json:"service"`
       Action     string                 `json:"action"`
       OnBehalfOf string                 `json:"on_behalf_of"`
       Params     map[string]interface{} `json:"params"`
     }
   - type ActResponse struct {
       Status  int                    `json:"status"`
       Service string                 `json:"service"`
       Action  string                 `json:"action"`
       Data    json.RawMessage        `json:"data"`
       Meta    map[string]interface{} `json:"meta"`
     }
   - HTTP handler for POST /v1/act
   - Orchestrates: validate → route → get token → build request → execute → respond
   - Uses standard net/http client with 30s timeout

5. Create configs/services/stripe.yaml, github.yaml, slack.yaml with 3-5 endpoints each.

6. Write tests:
   - internal/router/router_test.go — load configs, resolve known/unknown actions
   - internal/proxy/builder_test.go — path param substitution, GET vs POST body

ACCEPTANCE CRITERIA:
- `go test ./internal/router/... ./internal/proxy/...` passes
- Service configs load without error
- Path params correctly substituted
- GET params → query string, POST params → JSON body

When done, commit: "feat: implement service router and request builder"
```

---

## Phase 4: Auth Agent

**Trigger:** After Vault Agent commits.  
**Skill:** `@workspace` focused on `internal/auth/`, `internal/oauth/`, `internal/admin/`

### Instructions

```
You are the Auth Agent for agentgate. Vault and scaffold exist.

Implement API key auth, OAuth callback handling, and admin endpoints.

1. internal/auth/keys.go:
   - type AgentKey struct {
       ID              string
       Name            string
       KeyHash         string
       AllowedServices []string
       AllowedUsers    []string // "*" means all
       CreatedAt       time.Time
       RevokedAt       *time.Time
     }
   - func GenerateKey() (plaintext string, hash string, err error)
     - Format: "ag_live_" + 32 random hex chars
     - Hash with bcrypt
   - func (s *KeyStore) Create(ctx, name, services, users) (*AgentKey, plaintext, error)
   - func (s *KeyStore) Validate(ctx, plaintext) (*AgentKey, error)
   - func (s *KeyStore) Revoke(ctx, keyID) error

2. internal/auth/middleware.go:
   - func Middleware(store *KeyStore) func(http.Handler) http.Handler
   - Extract X-AgentGate-Key header
   - Validate against store
   - Check if key is revoked
   - Store AgentKey in request context
   - Return 401 with JSON error if invalid

3. internal/oauth/providers.go:
   - type OAuthProvider struct {
       Name         string
       ClientID     string
       ClientSecret string
       AuthorizeURL string
       TokenURL     string
       Scopes       []string
     }
   - func LoadProviders(cfg config) map[string]*OAuthProvider
   - Read client_id/secret from env vars referenced in config

4. internal/oauth/state.go:
   - func EncryptState(userID, service string, key []byte) (string, error)
   - func DecryptState(state string, key []byte) (userID, service string, err error)
   - Use same AES-256-GCM as vault, base64url encode

5. internal/oauth/handler.go:
   - GET /auth/callback/{service}
   - Decrypt state param → get userID + service
   - Exchange code for tokens using provider config
   - Store tokens in vault (encrypted)
   - Return HTML: "Account linked! You can close this tab."

6. internal/admin/handler.go:
   - POST /admin/keys — create new API key (returns plaintext once)
   - DELETE /admin/keys/{id} — revoke key
   - POST /admin/link — returns OAuth authorize URL
   - GET /admin/tokens/{user_id} — list linked services (no token values!)
   - Protected by a separate admin secret (env var AGENTGATE_ADMIN_SECRET)

7. Tests:
   - internal/auth/middleware_test.go — valid key passes, invalid blocks, revoked blocks
   - internal/oauth/state_test.go — round-trip encrypt/decrypt state

ACCEPTANCE CRITERIA:
- `go test ./internal/auth/... ./internal/oauth/... ./internal/admin/...` passes
- Middleware correctly blocks unauthorized requests
- OAuth state survives round-trip
- Admin endpoints return correct status codes

When done, commit: "feat: implement auth middleware, OAuth flow, admin endpoints"
```

---

## Phase 5: Integration Agent

**Trigger:** After Router Agent and Auth Agent commit.  
**Skill:** `@workspace` focused on `configs/services/` and integration tests

### Instructions

```
You are the Integration Agent for agentgate. Core components exist.

Wire everything together and create integration tests.

1. Update cmd/agentgate/main.go to wire all components:
   - Load config → open DB → run migrations
   - Initialize vault with encryption key
   - Load service configs → create router
   - Initialize key store
   - Load OAuth providers
   - Mount routes:
     - POST /v1/act → proxy handler (with auth middleware)
     - GET /auth/callback/{service} → OAuth handler
     - POST /admin/keys → admin handler
     - POST /admin/link → admin handler
     - GET /healthz → health check
   - Start server with graceful shutdown

2. internal/ratelimit/limiter.go:
   - type Limiter struct { buckets sync.Map }
   - func NewLimiter(configs map[string]RateConfig) *Limiter
   - func (l *Limiter) Allow(agentKeyID, service string) bool
   - Token bucket algorithm (stdlib rate.Limiter under the hood)
   - Middleware that wraps the /v1/act handler

3. internal/audit/logger.go:
   - func LogCall(db *sql.DB, entry AuditEntry) error
   - Called after every /v1/act response
   - Non-blocking (goroutine with buffered channel)

4. Create test/integration_test.go:
   - Uses httptest.Server
   - Tests the full flow:
     a. Create API key via admin endpoint
     b. Store a mock token in vault (skip OAuth)
     c. Call POST /v1/act with the key
     d. Mock upstream SaaS with httptest (return canned response)
     e. Verify response matches upstream
     f. Verify audit log has entry
   - Test rate limiting (send burst, expect 429)
   - Test expired token + refresh

5. Create configs/services/ YAML files with real endpoint definitions:
   
   stripe.yaml — endpoints:
   - GET /v1/invoices
   - GET /v1/customers
   - POST /v1/charges
   - GET /v1/balance
   
   github.yaml — endpoints:
   - GET /repos/{owner}/{repo}/issues
   - POST /repos/{owner}/{repo}/issues
   - GET /user/repos
   - GET /repos/{owner}/{repo}/pulls
   
   slack.yaml — endpoints:
   - POST /api/chat.postMessage (note: Slack uses POST for reads too)
   - GET /api/conversations.list
   - GET /api/users.list
   - POST /api/reactions.add

ACCEPTANCE CRITERIA:
- `go test ./...` passes (all unit + integration)
- `go build ./cmd/agentgate` succeeds
- `docker-compose up --build` starts without error
- `curl localhost:8080/healthz` returns 200
- Full integration test demonstrates agent → gateway → mock SaaS flow

When done, commit: "feat: wire components, add rate limiting, audit, integration tests"
```

---

## Phase 6: SDK Agent

**Trigger:** After Integration Agent commits.  
**Skill:** `@workspace` focused on `pkg/sdk/`

### Instructions

```
You are the SDK Agent for agentgate. The gateway is fully functional.

Create a Go SDK that agents import to talk to the gateway.

1. pkg/sdk/client.go:
   - type Client struct {
       baseURL    string
       apiKey     string
       httpClient *http.Client
     }
   - func NewClient(baseURL, apiKey string, opts ...Option) *Client
   - type Option func(*Client)
   - func WithHTTPClient(c *http.Client) Option
   - func WithTimeout(d time.Duration) Option

2. pkg/sdk/act.go:
   - type ActRequest struct {
       Service    string                 `json:"service"`
       Action     string                 `json:"action"`
       OnBehalfOf string                 `json:"on_behalf_of"`
       Params     map[string]interface{} `json:"params,omitempty"`
     }
   - type ActResponse struct {
       Status  int             `json:"status"`
       Service string          `json:"service"`
       Action  string          `json:"action"`
       Data    json.RawMessage `json:"data"`
       Meta    Meta            `json:"meta"`
       Error   string          `json:"error,omitempty"`
       Message string          `json:"message,omitempty"`
     }
   - type Meta struct {
       LatencyMs int  `json:"latency_ms"`
       Cached    bool `json:"cached"`
     }
   - func (c *Client) Act(ctx context.Context, req ActRequest) (*ActResponse, error)

3. pkg/sdk/helpers.go — convenience methods:
   - func (c *Client) Stripe(ctx, userID, action string, params map[string]interface{}) (*ActResponse, error)
   - func (c *Client) GitHub(ctx, userID, action string, params map[string]interface{}) (*ActResponse, error)
   - func (c *Client) Slack(ctx, userID, action string, params map[string]interface{}) (*ActResponse, error)

4. pkg/sdk/errors.go:
   - type AgentGateError struct { Status int; Code string; Message string; RelinkURL string }
   - func (e *AgentGateError) Error() string
   - Parse error responses into typed errors

5. Create examples/basic/main.go:
   - Demo script showing SDK usage
   - Create client, call Stripe list invoices, print result

6. Tests:
   - pkg/sdk/client_test.go — mock server, test Act method, test error handling

ACCEPTANCE CRITERIA:
- `go test ./pkg/sdk/...` passes
- Example compiles: `go build ./examples/basic`
- SDK correctly handles success + error responses
- Clean, idiomatic Go API

When done, commit: "feat: add Go SDK for agent consumers"
```

---

## Phase 7: QA Agent

**Trigger:** After all above commit.  
**Skill:** `@workspace` full repo access

### Instructions

```
You are the QA Agent for agentgate. The full system is built.

Your job: verify everything works end-to-end, fix any issues, write README.

1. Run `go test ./...` — fix any failures.

2. Run `go vet ./...` — fix any issues.

3. Run `docker-compose up --build` — verify it starts clean.

4. Create a comprehensive README.md:
   - One-line description
   - Architecture diagram (ASCII)
   - Quick start (docker-compose up)
   - API reference (POST /v1/act, POST /admin/keys, POST /admin/link)
   - SDK usage example
   - Security model summary
   - Configuration reference
   - Contributing section

5. Create a .env.example file with all required env vars documented.

6. Verify these scenarios manually (using curl in terminal):
   - GET /healthz → 200
   - POST /admin/keys → creates key
   - POST /v1/act without key → 401
   - POST /v1/act with key but unknown service → 404
   - POST /v1/act with key + known service but no token → appropriate error

7. If any test fails or code doesn't compile, FIX IT. You have full write access.

8. Final commit: "docs: add README, .env.example, verify build"

ACCEPTANCE CRITERIA:
- `go test ./...` passes with 0 failures
- `go build ./cmd/agentgate` succeeds
- `docker-compose up --build` runs without error
- README is comprehensive and accurate
- .env.example lists all vars
```

---

## Execution Order (Copilot Agent Mode)

```
PHASE 1: Architect Agent        [no deps]
    ↓
PHASE 2: Vault Agent            [depends on Phase 1]
PHASE 3: Router Agent           [depends on Phase 1] ← can run parallel with Phase 2
    ↓
PHASE 4: Auth Agent             [depends on Phase 2]
    ↓
PHASE 5: Integration Agent      [depends on Phase 3 + Phase 4]
    ↓
PHASE 6: SDK Agent              [depends on Phase 5]
    ↓
PHASE 7: QA Agent               [depends on all above]
```

---

## Master Prompt (paste into Copilot Chat to kick off)

```
I'm building agentgate — a thin API gateway that lets AI agents call SaaS APIs (Stripe, GitHub, Slack) on behalf of users. Agents never see tokens. Gateway handles OAuth + token vault.

Tech: Go 1.22, chi router, SQLite, Docker. Repo: ~/clawdlinux/agentgate

Execute these phases IN ORDER. Each phase builds on the previous. Do NOT skip ahead.
After each phase, run `go build ./cmd/agentgate` and `go test ./...` to verify.
If tests fail, fix them before moving to the next phase.

PHASE 1 — SCAFFOLD:
[paste Phase 1 instructions above]

PHASE 2 — TOKEN VAULT:
[paste Phase 2 instructions above]

PHASE 3 — ROUTER:
[paste Phase 3 instructions above]

PHASE 4 — AUTH:
[paste Phase 4 instructions above]

PHASE 5 — INTEGRATION:
[paste Phase 5 instructions above]

PHASE 6 — SDK:
[paste Phase 6 instructions above]

PHASE 7 — QA:
[paste Phase 7 instructions above]

After all phases: run `docker-compose up --build` and verify the container starts. Then commit all work.
```

---

## .github/copilot-instructions.md (put in repo)

```markdown
# Copilot Instructions for AgentGate

## Project Context
AgentGate is a thin API gateway for AI agents. It proxies requests to SaaS APIs
(Stripe, GitHub, Slack) on behalf of users, handling OAuth token storage and injection.

## Architecture
- Go 1.22 with chi router
- SQLite for storage (MVP)
- AES-256-GCM for token encryption
- Token bucket rate limiting
- Structured logging with zerolog

## Code Style
- Standard Go project layout (cmd/, internal/, pkg/)
- Interfaces for testability (especially vault, router)
- Table-driven tests
- Context propagation everywhere
- Errors wrapped with fmt.Errorf("component: %w", err)
- No global state — dependency injection via constructors

## Key Design Decisions
- Single endpoint POST /v1/act — simplicity over REST purity
- Agent never sees tokens — security by architecture
- Service configs are YAML, not full OpenAPI — keep it simple
- SQLite for MVP — easy to swap for Postgres later via interface

## Testing
- Unit tests alongside implementation files
- Integration tests in test/ directory
- Use httptest for HTTP testing
- Use in-memory SQLite for test isolation

## What NOT to do
- Don't add a web UI (Phase 2)
- Don't add WebSocket support yet
- Don't add caching yet
- Don't use an ORM — raw SQL is fine for 3 tables
- Don't add gRPC — HTTP JSON only for MVP
```
