/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

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
