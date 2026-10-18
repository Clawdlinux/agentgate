-- Store companion producer records separately from frozen gateway receipt fields.
CREATE TABLE receipt_records (
    seq INTEGER PRIMARY KEY REFERENCES receipts(seq),
    record_json BLOB NOT NULL
);
