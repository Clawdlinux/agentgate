# .github/copilot-instructions.md

## Project: AgentGate

AgentGate is a thin API gateway that lets AI agents authenticate to and call SaaS APIs on behalf of humans. Agents never see OAuth tokens. The gateway handles token storage (AES-256-GCM encrypted), auto-refresh, and request proxying.

## Tech Stack
- Go 1.22+ (single binary)
- chi/v5 router
- SQLite via go-sqlite3 (CGO required)
- zerolog for structured logging
- golang.org/x/time/rate for rate limiting
- Docker for deployment

## Architecture
```
Agent → POST /v1/act → [Auth MW] → [Router] → [Vault: get token] → [Proxy: call upstream] → Response
```

Single entry point. Agent sends service + action + user_id. Gateway resolves route, fetches encrypted token, injects into upstream request, returns response.

## Code Conventions
- Standard Go project layout: cmd/, internal/, pkg/
- No global state. All dependencies injected via constructors.
- Interfaces for testability (VaultInterface, RouterInterface)
- Table-driven tests with subtests
- Context propagation on all DB/HTTP calls
- Errors: `fmt.Errorf("package.func: %w", err)`
- JSON responses for all endpoints (including errors)
- HTTP status codes: 200 success, 400 bad request, 401 unauthorized, 404 not found, 429 rate limited, 500 internal

## Database
SQLite with 3 tables: agent_keys, tokens, audit_log. Raw SQL (no ORM). Migrations in internal/db/migrations/.

## Service Configs
YAML files in configs/services/. Each defines: name, base_url, auth_type, endpoints[]. Endpoints have action string (e.g., "GET /v1/invoices") mapped to upstream path.

## Security
- API keys: bcrypt hashed, scoped to services + users
- Tokens: AES-256-GCM encrypted at rest, 32-byte key from env
- OAuth state: encrypted + time-limited (10min)
- Admin endpoints: separate secret header
- Audit: every proxied call logged

## Testing Strategy
- Unit tests: internal packages, in-memory SQLite
- Integration tests: test/ directory, full HTTP flow with httptest
- Mock upstream SaaS with httptest servers

## What NOT to Build (out of MVP scope)
- Web UI / dashboard
- WebSocket streaming
- Response caching
- gRPC interface
- ORM / query builder
- Agent-to-agent communication
- Multi-tenant isolation (single-tenant MVP)
