package vault

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			service TEXT NOT NULL,
			access_token_enc BLOB NOT NULL,
			refresh_token_enc BLOB,
			expires_at DATETIME,
			scopes TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, service)
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteStore_PutGet(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewSQLiteStore(db, testKey())
	if err != nil {
		t.Fatal(err)
	}

	tok := Token{
		AccessToken:  "sqlite-access-123",
		RefreshToken: "sqlite-refresh-456",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
		Scopes:       []string{"read", "write"},
	}

	if err := store.Put("user-1", "github", tok); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get("user-1", "github")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AccessToken != "sqlite-access-123" {
		t.Fatalf("access_token = %s", got.AccessToken)
	}
	if got.RefreshToken != "sqlite-refresh-456" {
		t.Fatalf("refresh_token = %s", got.RefreshToken)
	}
}

func TestSQLiteStore_Upsert(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, _ := NewSQLiteStore(db, testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "old"})
	_ = store.Put("user-1", "github", Token{AccessToken: "new"})

	got, _ := store.Get("user-1", "github")
	if got.AccessToken != "new" {
		t.Fatalf("expected upsert, got %s", got.AccessToken)
	}
}

func TestSQLiteStore_NotFound(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, _ := NewSQLiteStore(db, testKey())

	_, err := store.Get("nobody", "nothing")
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, _ := NewSQLiteStore(db, testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "x"})
	_ = store.Delete("user-1", "github")

	_, err := store.Get("user-1", "github")
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestSQLiteStore_ListServices(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, _ := NewSQLiteStore(db, testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "a"})
	_ = store.Put("user-1", "stripe", Token{AccessToken: "b"})

	services, err := store.ListServices("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %v, want 2", services)
	}
}

func TestSQLiteStore_EncryptionAtRest(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, _ := NewSQLiteStore(db, testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "super-secret-value"})

	var raw []byte
	db.QueryRow("SELECT access_token_enc FROM tokens WHERE user_id='user-1' AND service='github'").Scan(&raw)

	for i := 0; i < len(raw)-17; i++ {
		if string(raw[i:i+18]) == "super-secret-value" {
			t.Fatal("plaintext found in DB — encryption failed")
		}
	}
}
