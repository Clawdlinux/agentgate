CREATE TABLE IF NOT EXISTS delegation_root_key (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    public_key BLOB NOT NULL,
    private_key_enc BLOB NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
