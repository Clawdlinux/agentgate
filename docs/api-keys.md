# API keys

New keys use `agk_<keyid>_<secret>`. Both components use lowercase hex without padding.
The ID contains 16 random bytes, encoded as 32 characters.
The secret contains 32 random bytes, encoded as 64 characters.
Both come from `crypto/rand`. Save the complete key when it is returned.

SQLite stores `key_id` and `key_hmac`, not the secret.
`key_hmac` is HMAC-SHA256 over the decoded secret bytes.
The pepper comes from HKDF-SHA256 over the existing 32-byte `AGENTGATE_VAULT_KEY`.
HKDF uses no salt and the info string `agentgate api-key pepper v1`.
There is no new environment variable.

Validation performs 1 indexed lookup by `key_id` and compares the HMAC with `hmac.Equal`.
Unknown IDs still compute 1 HMAC against a dummy value.
Malformed keys never reach SQLite or bcrypt.

Verified keys have a process-local LRU cache.
Defaults are 1024 entries and a 30-second TTL.
Cache keys are SHA-256 digests of complete presented keys, never plaintext.
`WithCacheSize` and `WithCacheTTL` configure each store.
Setting either value to 0 disables caching.
`KeyStore.Revoke` invalidates that store's cached entry immediately, including concurrent validations.
Other processes, other stores, and direct SQL updates can retain cached permissions until the TTL expires.

## Existing databases

Migration `005_key_lookup.sql` adds nullable columns and a unique index.
Existing rows keep their bcrypt hashes and NULL lookup columns.
Migration files run transactionally and are tracked in `schema_migrations`.
Existing databases without migration history are upgraded automatically.

Legacy keys use `ag_live_` followed by exactly 64 lowercase hex characters.
They still validate through bcrypt and successful validations are cached.
The gateway warns once per legacy key ID per process. It never logs the presented key.
Well-formed legacy misses still scan legacy rows. Replace these keys to remove that CPU cost.

Create a replacement through `POST /admin/keys`, update your client, then revoke the old key.
New key creation never emits legacy keys.
Keep `AGENTGATE_VAULT_KEY` unchanged across restarts.
Changing it invalidates new keys and prevents decrypting existing vault records.
