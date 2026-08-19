/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package auth

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE agent_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL,
			allowed_services TEXT NOT NULL DEFAULT '["*"]',
			allowed_users TEXT NOT NULL DEFAULT '["*"]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			revoked_at DATETIME
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestKeyStore_CountStartsAtZero(t *testing.T) {
	t.Parallel()
	store := NewKeyStore(testDB(t))

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
	store := NewKeyStore(testDB(t))

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
