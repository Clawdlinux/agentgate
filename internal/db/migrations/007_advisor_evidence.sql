-- Receipt v1 stays unchanged. Records are bound through params_sha256.
CREATE TABLE receipt_records (
    seq INTEGER PRIMARY KEY REFERENCES receipts(seq),
    record_json BLOB NOT NULL
);
