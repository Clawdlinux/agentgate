CREATE TABLE IF NOT EXISTS signer_keys (
    kid TEXT PRIMARY KEY,
    public_key BLOB NOT NULL,
    private_key_enc BLOB NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    valid_from_seq INTEGER NOT NULL,
    valid_until_seq INTEGER,
    active INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_signer_keys_one_active
    ON signer_keys(active) WHERE active = 1;
