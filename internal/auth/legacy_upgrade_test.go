package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func insertLegacyKey(t testing.TB, store *KeyStore, id, plaintext string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		"INSERT INTO agent_keys (id, name, key_hash) VALUES (?, 'legacy', ?)", id, string(hash)); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyUpgradeUsesIndexedLookup(t *testing.T) {
	store := newTestKeyStore(t, testDB(t), WithCacheSize(0))
	plaintext := "ag_live_" + strings.Repeat("b", 64)
	insertLegacyKey(t, store, "legacy", plaintext)
	calls := countBcrypt(store)
	for i := 0; i < 2; i++ {
		if _, err := store.Validate(t.Context(), plaintext); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("bcrypt calls = %d, want 1", calls.Load())
	}
	var lookup []byte
	if err := store.db.QueryRowContext(t.Context(),
		"SELECT legacy_lookup FROM agent_keys WHERE id = 'legacy'").Scan(&lookup); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, store.pepper)
	mac.Write([]byte(plaintext))
	if !hmac.Equal(lookup, mac.Sum(nil)) {
		t.Fatal("legacy lookup must authenticate the full presented key")
	}
	database := &countedDB{DB: store.db.(*sql.DB)}
	other := newTestKeyStore(t, database.DB, WithCacheSize(0))
	other.db = database
	if _, err := other.Validate(t.Context(), plaintext); err != nil {
		t.Fatal(err)
	}
	if database.lookups.Load() != 1 || database.scans.Load() != 0 {
		t.Fatal("upgraded key did not use one indexed lookup")
	}
}

func TestLegacyMissScansOnlyActiveUnupgradedKeys(t *testing.T) {
	for _, count := range []int{1, 20} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := newTestKeyStore(t, testDB(t))
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("legacy-%d", i)
				digest := sha256.Sum256([]byte(id))
				for _, state := range []string{"active", "revoked", "upgraded"} {
					var lookup any
					var revoked any
					if state == "upgraded" {
						lookup = digest[:]
					}
					if state == "revoked" {
						revoked = time.Now()
					}
					if _, err := store.db.ExecContext(t.Context(),
						"INSERT INTO agent_keys (id, name, key_hash, legacy_lookup, revoked_at) VALUES (?, 'legacy', 'unused', ?, ?)",
						id+"-"+state, lookup, revoked); err != nil {
						t.Fatal(err)
					}
				}
			}
			var calls atomic.Int64
			store.compareLegacy = func(_, _ []byte) error {
				calls.Add(1)
				return bcrypt.ErrMismatchedHashAndPassword
			}
			miss := "ag_live_" + strings.Repeat("0", 64)
			if _, err := store.Validate(t.Context(), miss); !errors.Is(err, ErrKeyNotFound) {
				t.Fatal(err)
			}
			if calls.Load() != int64(count) {
				t.Fatalf("bcrypt calls = %d, want %d", calls.Load(), count)
			}
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("legacy-%d-active", i)
				digest := sha256.Sum256([]byte(id))
				if _, err := store.db.ExecContext(t.Context(),
					"UPDATE agent_keys SET legacy_lookup = ? WHERE id = ?", digest[:], id); err != nil {
					t.Fatal(err)
				}
			}
			calls.Store(0)
			if _, err := store.Validate(t.Context(), miss); !errors.Is(err, ErrKeyNotFound) {
				t.Fatal(err)
			}
			if calls.Load() != 0 {
				t.Fatal("upgraded or revoked rows reached bcrypt")
			}
		})
	}
}

func TestLegacySemaphoreIsProcessWideAndNonBlocking(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	miss := "ag_live_" + strings.Repeat("0", 64)
	for i := 0; i < 2; i++ {
		store := newTestKeyStore(t, testDB(t))
		insertLegacyKey(t, store, fmt.Sprint(i), "ag_live_"+strings.Repeat("a", 64))
		store.compareLegacy = func(_, _ []byte) error {
			entered <- struct{}{}
			<-release
			return bcrypt.ErrMismatchedHashAndPassword
		}
		wg.Go(func() {
			if _, err := store.Validate(t.Context(), miss); !errors.Is(err, ErrKeyNotFound) {
				t.Errorf("slow validation: %v", err)
			}
		})
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("legacy validation did not enter bcrypt")
		}
	}
	store := newTestKeyStore(t, testDB(t))
	insertLegacyKey(t, store, "third", "ag_live_"+strings.Repeat("a", 64))
	calls := countBcrypt(store)
	done := make(chan error, 1)
	wg.Go(func() {
		_, err := store.Validate(t.Context(), miss)
		done <- err
	})
	select {
	case err := <-done:
		if !errors.Is(err, ErrKeyNotFound) || calls.Load() != 0 {
			t.Fatalf("saturated validation: err=%v bcrypt=%d", err, calls.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("saturated validation blocked")
	}
}

func TestSlowLegacyValidationDoesNotBlockCacheHitsOrRevoke(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	_, modern, err := store.Create(t.Context(), "modern", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(t.Context(), modern); err != nil {
		t.Fatal(err)
	}
	legacy := "ag_live_" + strings.Repeat("a", 64)
	insertLegacyKey(t, store, "slow", legacy)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	store.compareLegacy = func(_, _ []byte) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	slowDone := make(chan error, 1)
	wg.Go(func() {
		_, err := store.Validate(t.Context(), legacy)
		slowDone <- err
	})
	<-entered
	hit := make(chan error, 1)
	revoked := make(chan error, 1)
	wg.Go(func() { revoked <- store.Revoke(t.Context(), "slow") })
	wg.Go(func() {
		_, err := store.Validate(t.Context(), modern)
		hit <- err
	})
	for name, result := range map[string]<-chan error{"cache hit": hit, "revoke": revoked} {
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s blocked on legacy bcrypt", name)
		}
	}
	close(release)
	wg.Wait()
	if err := <-slowDone; !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("in-flight revoked legacy validation: %v", err)
	}
	if _, err := store.Validate(t.Context(), legacy); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("revoked legacy validation: %v", err)
	}
}
