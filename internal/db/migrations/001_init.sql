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
    refresh_token_enc BLOB,
    expires_at DATETIME,
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

CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit_log(agent_key_id);
CREATE INDEX IF NOT EXISTS idx_tokens_user_service ON tokens(user_id, service);
