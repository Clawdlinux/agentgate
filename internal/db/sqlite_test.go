/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenDurabilityPragmasOnEveryConnection(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "durable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// Hold both connections so the second check cannot reuse the first connection.
	for range 2 {
		connection, err := database.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		var journal string
		if err := connection.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&journal); err != nil {
			t.Fatal(err)
		}
		if journal != "wal" {
			t.Fatalf("journal_mode=%q, want wal", journal)
		}
		for query, want := range map[string]int{"PRAGMA synchronous": 2, "PRAGMA busy_timeout": 5000, "PRAGMA foreign_keys": 1} {
			var got int
			if err := connection.QueryRowContext(t.Context(), query).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("%s=%d, want %d", query, got, want)
			}
		}
	}
}

func TestRunMigrations_AppliesAllMigrationsCleanly(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"agent_keys", "tokens", "audit_log", "signer_keys", "orgs", "org_admins"} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %s: exists=%d, want 1", table, count)
		}
	}

	// RunMigrations must be idempotent (CREATE TABLE/INDEX IF NOT EXISTS).
	if err := RunMigrations(database); err != nil {
		t.Fatalf("second RunMigrations call failed: %v", err)
	}
}
