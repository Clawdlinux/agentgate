ALTER TABLE agent_keys ADD COLUMN key_id TEXT;
ALTER TABLE agent_keys ADD COLUMN key_hmac BLOB;

CREATE UNIQUE INDEX idx_agent_keys_key_id ON agent_keys(key_id);
