CREATE TABLE IF NOT EXISTS receipts (
    seq INTEGER PRIMARY KEY,
    format_version INTEGER NOT NULL DEFAULT 1,
    timestamp_unix_ns INTEGER NOT NULL,
    human_principal TEXT NOT NULL,
    agent_key_id TEXT NOT NULL,
    delegation_chain_json TEXT NOT NULL DEFAULT '[]',
    service TEXT NOT NULL,
    action TEXT NOT NULL,
    params_sha256 BLOB NOT NULL,
    policy_decision TEXT NOT NULL CHECK (policy_decision IN ('allow','deny','rate_limited')),
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    prev_hash BLOB NOT NULL,
    entry_hash BLOB NOT NULL UNIQUE,
    signer_kid TEXT NOT NULL REFERENCES signer_keys(kid),
    signature BLOB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_receipts_timestamp ON receipts(timestamp_unix_ns);
CREATE INDEX IF NOT EXISTS idx_receipts_agent ON receipts(agent_key_id);
CREATE INDEX IF NOT EXISTS idx_receipts_service_action ON receipts(service, action);

CREATE TRIGGER IF NOT EXISTS trg_receipts_no_update
BEFORE UPDATE ON receipts
BEGIN
    SELECT RAISE(ABORT, 'receipts are append-only: UPDATE is not permitted');
END;

CREATE TRIGGER IF NOT EXISTS trg_receipts_no_delete
BEFORE DELETE ON receipts
BEGIN
    SELECT RAISE(ABORT, 'receipts are append-only: DELETE is not permitted');
END;
