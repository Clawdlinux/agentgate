# API keys

New keys use `agk_<keyid>_<secret>`. Both components use lowercase hex without padding.
The ID contains 16 random bytes, encoded as 32 characters.
The secret contains 32 random bytes, encoded as 64 characters.
Both come from `crypto/rand`. Save the complete key when it is returned.

SQLite stores `key_id` and `key_hmac`, not the secret.
`key_hmac` is HMAC-SHA256 over the decoded secret bytes.
The pepper comes from HKDF-SHA256 over the existing 32-byte `AGENTGATE_VAULT_KEY`.
HKDF uses no salt and the info string `agentgate api-key pepper v1`.
Pepper derivation needs no extra secret.

Validation performs 1 indexed lookup by `key_id` and compares the HMAC with `hmac.Equal`.
Unknown IDs still compute 1 HMAC against a dummy value.
Malformed keys never reach SQLite or bcrypt.

Verified keys have a process-local LRU cache.
Defaults are 1024 entries and a 30-second TTL.
Cache keys are SHA-256 digests of complete presented keys, never plaintext.
`WithCacheSize` and `WithCacheTTL` configure each store.
Setting either value to 0 disables caching.
The gateway also accepts `AGENTGATE_API_KEY_CACHE_TTL`, a duration such as `5s` or `1m`.
It defaults to `30s`. Set it to `0` to disable caching.
Negative or invalid durations prevent startup.
`KeyStore.Revoke` invalidates that store's cached entry immediately.
A revocation generation prevents in-flight validations from repopulating stale cache entries.
Database calls and bcrypt never hold the cache mutex. Slow legacy checks cannot block cache hits or revocation.
Other processes, other stores, and direct SQL updates can retain cached permissions until the TTL expires.

## Existing databases

Migration `005_key_lookup.sql` adds nullable new-format columns and a unique index.
Migration `006_legacy_lookup.sql` adds nullable `legacy_lookup` and its unique index.
Its partial index keeps revoked and upgraded rows out of fallback query work.
Existing legacy rows keep their bcrypt hashes. Their new lookup columns start as NULL.
Migration files run transactionally and are tracked in `schema_migrations`.
Existing databases without migration history are upgraded automatically.

Legacy keys use `ag_live_` followed by exactly 64 lowercase hex characters.
Validation first checks an indexed HMAC-SHA256 of the full presented legacy key.
If no active row matches, bcrypt checks only active rows without a legacy lookup.
The first successful bcrypt check persists that lookup. Later checks no longer need bcrypt, even after restart.
Successful validations also enter the verified-key cache.
Revoked legacy keys return `ErrKeyNotFound`. Revoked rows never enter the bcrypt scan.
The gateway warns once per legacy key ID per process. It never logs the presented key.
At most 2 uncached legacy validations run per process. Excess attempts fail immediately with `ErrKeyNotFound`.
Saturation logs use debug level, contain no key material, and occur at most once per minute.
Unupgraded active rows still incur bcrypt work on misses.
Once every legacy row is upgraded or revoked, legacy misses perform zero bcrypt calls.

Create a replacement through `POST /admin/keys`, update your client, then revoke the old key.
New key creation never emits legacy keys.
Keep `AGENTGATE_VAULT_KEY` unchanged across restarts.
Changing it invalidates new and upgraded legacy keys. It also prevents decrypting existing vault records.
