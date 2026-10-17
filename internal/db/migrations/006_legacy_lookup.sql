ALTER TABLE agent_keys ADD COLUMN legacy_lookup BLOB;

CREATE UNIQUE INDEX idx_agent_keys_legacy_lookup ON agent_keys(legacy_lookup);

CREATE INDEX idx_agent_keys_legacy_pending ON agent_keys(key_id, legacy_lookup)
    WHERE key_id IS NULL AND legacy_lookup IS NULL AND revoked_at IS NULL;
