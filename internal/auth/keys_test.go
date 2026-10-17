/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func testDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := agentgatedb.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTestKeyStore(t testing.TB, db *sql.DB, options ...KeyStoreOption) *KeyStore {
	t.Helper()
	pepper, err := DeriveKeyPepper(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewKeyStore(db, pepper, options...)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestKeyStore_CountStartsAtZero(t *testing.T) {
	t.Parallel()
	store := newTestKeyStore(t, testDB(t))

	n, err := store.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("Count = %d, want 0", n)
	}
}

func TestKeyStore_CountIncludesRevokedKeys(t *testing.T) {
	t.Parallel()
	store := newTestKeyStore(t, testDB(t))

	id, _, err := store.Create(t.Context(), "agent-1", []string{"*"}, []string{"*"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Revoke(t.Context(), id.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	n, err := store.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Fatalf("Count = %d, want 1 (revoked keys still count)", n)
	}
}

type countedDB struct {
	*sql.DB
	lookups atomic.Int64
	scans   atomic.Int64
}

func (db *countedDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db.lookups.Add(1)
	return db.DB.QueryRowContext(ctx, query, args...)
}

func (db *countedDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db.scans.Add(1)
	return db.DB.QueryContext(ctx, query, args...)
}

func countBcrypt(store *KeyStore) *atomic.Int64 {
	count := new(atomic.Int64)
	store.compareLegacy = func(hash, key []byte) error {
		count.Add(1)
		return bcrypt.CompareHashAndPassword(hash, key)
	}
	return count
}

func TestKeyStoreRoundTripAndRevoke(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	database := &countedDB{DB: store.db.(*sql.DB)}
	store.db = database
	key, plaintext, err := store.Create(t.Context(), "agent", []string{"github"}, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(plaintext, "_")
	if len(parts) != 3 || parts[0] != "agk" || len(parts[1]) != 32 || len(parts[2]) != 64 {
		t.Fatalf("unexpected key format")
	}
	secret, err := hex.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	var storedID, storedHash string
	var storedHMAC []byte
	if err := database.DB.QueryRow("SELECT key_id, key_hash, key_hmac FROM agent_keys WHERE id = ?", key.ID).
		Scan(&storedID, &storedHash, &storedHMAC); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, store.pepper)
	mac.Write(secret)
	if storedID != parts[1] || storedHash != "" || !hmac.Equal(storedHMAC, mac.Sum(nil)) {
		t.Fatal("stored key must contain only its identifier and HMAC")
	}
	for i := 0; i < 2; i++ {
		got, err := store.Validate(t.Context(), plaintext)
		if err != nil || got.ID != key.ID || !got.CanAccessService("github") || !got.CanAccessUser("alice") {
			t.Fatalf("Validate = (%v, %v)", got, err)
		}
		if got.CanAccessService("other") || got.CanAccessUser("other") {
			t.Fatal("caller mutated cached permissions")
		}
		got.AllowedServices[0] = "*"
		got.AllowedUsers[0] = "*"
	}
	if database.lookups.Load() != 1 {
		t.Fatal("verified key was not cached")
	}
	wrong := plaintext[:len(plaintext)-1] + "0"
	if wrong == plaintext {
		wrong = plaintext[:len(plaintext)-1] + "1"
	}
	if _, err := store.Validate(t.Context(), wrong); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("wrong secret: %v", err)
	}
	if err := store.Revoke(t.Context(), key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(t.Context(), plaintext); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("cached revoked key: %v", err)
	}
	if _, err := store.Validate(t.Context(), wrong); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("wrong secret for revoked key: %v", err)
	}
}

func TestKeyStoreMalformedSkipsBcryptAndDatabase(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	database := &countedDB{DB: store.db.(*sql.DB)}
	store.db = database
	bcryptCalls := countBcrypt(store)
	id, secret := strings.Repeat("a", 32), strings.Repeat("b", 64)
	for _, input := range []string{
		"", "not-a-key", "agk_" + id + "_" + secret[:63],
		"agk_" + id[:31] + "_" + secret, "agk_" + id + "_" + secret + "0",
		"agk_" + strings.Repeat("g", 32) + "_" + secret,
		"agk_" + id + "_" + strings.Repeat("g", 64),
		"agk_" + strings.ToUpper(id) + "_" + secret,
		"agk_" + id + "_" + strings.ToUpper(secret),
		"ag_live_", "ag_live_" + secret[:63], "ag_live_" + secret + "0",
		"ag_live_" + strings.Repeat("z", 64), strings.Repeat("x", 4096),
	} {
		if _, err := store.Validate(t.Context(), input); !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("malformed input returned %v", err)
		}
	}
	if bcryptCalls.Load() != 0 || database.lookups.Load() != 0 || database.scans.Load() != 0 {
		t.Fatal("malformed input reached bcrypt or the database")
	}
}

func TestKeyStoreInvalidCostIsIndependentOfKeyCount(t *testing.T) {
	for _, count := range []int{1, 200} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := newTestKeyStore(t, testDB(t))
			var existing string
			for i := 0; i < count; i++ {
				_, plaintext, err := store.Create(t.Context(), "agent", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				existing = plaintext
				if _, err := store.db.ExecContext(t.Context(),
					"INSERT INTO agent_keys (id, name, key_hash) VALUES (?, 'legacy', 'unused-bcrypt')",
					fmt.Sprintf("legacy-%d", i)); err != nil {
					t.Fatal(err)
				}
			}

			database := &countedDB{DB: store.db.(*sql.DB)}
			store.db = database
			bcryptCalls := countBcrypt(store)
			unknown := "agk_" + strings.Repeat("0", 32) + "_" + strings.Repeat("0", 64)
			wrongSecret := existing[:len(existing)-1] + "0"
			if wrongSecret == existing {
				wrongSecret = existing[:len(existing)-1] + "1"
			}
			for _, input := range []string{unknown, wrongSecret} {
				database.lookups.Store(0)
				if _, err := store.Validate(t.Context(), input); !errors.Is(err, ErrKeyNotFound) {
					t.Fatalf("Validate = %v", err)
				}
				if bcryptCalls.Load() != 0 || database.lookups.Load() != 1 || database.scans.Load() != 0 {
					t.Fatalf("bcrypt=%d lookups=%d scans=%d", bcryptCalls.Load(), database.lookups.Load(), database.scans.Load())
				}
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		plaintext, id, err := GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		gotID, secret, ok := parseKey(plaintext)
		if !ok || id != gotID || len(id) != 32 || secret == [32]byte{} {
			t.Fatal("generated key has invalid components")
		}
		if seen[id] {
			t.Fatal("generated duplicate key ID")
		}
		seen[id] = true
	}
}

func TestKeyStoreWrongPepperAndPepperOwnership(t *testing.T) {
	db := testDB(t)
	pepper := bytes.Repeat([]byte{1}, 32)
	store, err := NewKeyStore(db, pepper)
	if err != nil {
		t.Fatal(err)
	}
	_, plaintext, err := store.Create(t.Context(), "agent", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pepper[0] = 2
	if _, err := store.Validate(t.Context(), plaintext); err != nil {
		t.Fatalf("caller changed store pepper: %v", err)
	}
	other, err := NewKeyStore(db, pepper)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Validate(t.Context(), plaintext); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("wrong pepper: %v", err)
	}
}

func TestKeyStoreCacheOptions(t *testing.T) {
	for name, option := range map[string]KeyStoreOption{
		"size": WithCacheSize(0),
		"ttl":  WithCacheTTL(0),
	} {
		t.Run(name, func(t *testing.T) {
			store := newTestKeyStore(t, testDB(t), option)
			_, plaintext, err := store.Create(t.Context(), "agent", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			database := &countedDB{DB: store.db.(*sql.DB)}
			store.db = database
			for i := 0; i < 2; i++ {
				if _, err := store.Validate(t.Context(), plaintext); err != nil {
					t.Fatal(err)
				}
			}
			if database.lookups.Load() != 2 || len(store.cache) != 0 || store.lru.Len() != 0 {
				t.Fatal("disabled cache retained a key")
			}
		})
	}
	pepper := make([]byte, 32)
	for _, option := range []KeyStoreOption{WithCacheSize(-1), WithCacheTTL(-time.Second)} {
		if _, err := NewKeyStore(testDB(t), pepper, option); err == nil {
			t.Fatal("accepted a negative cache setting")
		}
	}
	store := newTestKeyStore(t, testDB(t))
	if store.cacheSize != 1024 || store.cacheTTL != 30*time.Second {
		t.Fatal("unexpected cache defaults")
	}
}

func TestKeyStoreCacheUsesFullKeyDigest(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	_, plaintext, err := store.Create(t.Context(), "agent", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(t.Context(), plaintext); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(plaintext))
	if len(store.cache) != 1 || store.cache[digest] == nil {
		t.Fatal("cache must use a digest of the complete key")
	}
	for i := 0; i < 2; i++ {
		unknown := "agk_" + strings.Repeat("0", 32) + "_" + strings.Repeat("0", 64)
		if _, err := store.Validate(t.Context(), unknown); !errors.Is(err, ErrKeyNotFound) {
			t.Fatal(err)
		}
	}
	if len(store.cache) != 1 {
		t.Fatal("failed validations must not enter the cache")
	}
}
func TestKeyStoreLegacy(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	plaintext := "ag_live_" + strings.Repeat("a", 64)
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	_, keyID, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	id := "legacy-" + keyID
	if _, err := store.db.ExecContext(t.Context(),
		"INSERT INTO agent_keys (id, name, key_hash) VALUES (?, ?, ?)", id, "legacy", string(hash)); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	bcryptCalls := countBcrypt(store)
	for i := 0; i < 2; i++ {
		got, err := store.Validate(t.Context(), plaintext)
		if err != nil || got.ID != id {
			t.Fatalf("legacy Validate = (%v, %v)", got, err)
		}
	}
	if bcryptCalls.Load() != 1 {
		t.Fatal("legacy success was not cached")
	}
	other := newTestKeyStore(t, store.db.(*sql.DB))
	if _, err := other.Validate(t.Context(), plaintext); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), "deprecated") != 1 || strings.Contains(logs.String(), plaintext) ||
		!strings.Contains(logs.String(), id) {
		t.Fatalf("unexpected legacy deprecation logging: %s", logs.String())
	}
	if err := store.Revoke(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(t.Context(), plaintext); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("legacy revoke: %v", err)
	}
}

func TestKeyStoreCacheTTLAndLRU(t *testing.T) {
	store := newTestKeyStore(t, testDB(t), WithCacheTTL(time.Second), WithCacheSize(2))
	now := time.Now()
	store.now = func() time.Time { return now }
	database := &countedDB{DB: store.db.(*sql.DB)}
	store.db = database
	keys := make([]string, 3)
	for i := range keys {
		_, key, err := store.Create(t.Context(), "agent", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = key
	}
	validate := func(i int) {
		t.Helper()
		if _, err := store.Validate(t.Context(), keys[i]); err != nil {
			t.Fatal(err)
		}
	}
	for _, i := range []int{0, 1, 0, 2, 0} {
		validate(i)
	}
	if database.lookups.Load() != 3 {
		t.Fatal("cache hit did not refresh LRU ordering")
	}
	validate(1)
	if database.lookups.Load() != 4 {
		t.Fatal("least recently used entry was not evicted")
	}
	now = now.Add(time.Second)
	validate(1)
	if database.lookups.Load() != 5 {
		t.Fatal("TTL expiry did not force a lookup")
	}
}

func TestKeyStoreConcurrentValidateAndRevoke(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	key, plaintext, err := store.Create(t.Context(), "agent", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			for j := 0; j < 10; j++ {
				_, err := store.Validate(t.Context(), plaintext)
				if err != nil && !errors.Is(err, ErrKeyRevoked) {
					t.Errorf("Validate: %v", err)
				}
			}
		})
	}
	if err := store.Revoke(t.Context(), key.ID); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if _, err := store.Validate(t.Context(), plaintext); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("revoked key survived concurrent cache insertion: %v", err)
	}
}

func TestKeyPepper(t *testing.T) {
	master := bytes.Repeat([]byte{1}, 32)
	pepper, err := DeriveKeyPepper(master)
	if err != nil {
		t.Fatal(err)
	}
	// HKDF-SHA256 with no salt and the documented purpose string.
	want, _ := hex.DecodeString("83fabd3941fd899e9708cf7f985e02724a19b70ac8e4294ff45648e6b2920434")
	if !bytes.Equal(pepper, want) {
		t.Fatalf("pepper = %x", pepper)
	}
	for _, size := range []int{0, 31, 33} {
		if _, err := DeriveKeyPepper(make([]byte, size)); err == nil {
			t.Fatalf("accepted master key length %d", size)
		}
		if _, err := NewKeyStore(testDB(t), make([]byte, size)); err == nil {
			t.Fatalf("accepted pepper length %d", size)
		}
	}
}
