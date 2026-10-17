package db

import (
	"database/sql"
	"strings"
	"testing"
)

func TestLegacyLookupMigration(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec("CREATE TABLE schema_migrations (name TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "006" {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := applyMigration(database, entry.Name(), string(data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec("INSERT INTO agent_keys (id, name, key_hash) VALUES ('old', 'legacy', 'bcrypt')"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(database); err != nil {
			t.Fatal(err)
		}
	}
	var lookup []byte
	if err := database.QueryRow("SELECT legacy_lookup FROM agent_keys WHERE id = 'old'").Scan(&lookup); err != nil {
		t.Fatal(err)
	}
	if lookup != nil {
		t.Fatal("migration changed the legacy row")
	}
	if _, err := database.Exec("INSERT INTO agent_keys (id, name, key_hash) VALUES ('other', 'legacy', 'bcrypt')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE agent_keys SET legacy_lookup = X'01' WHERE id = 'old'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE agent_keys SET legacy_lookup = X'01' WHERE id = 'other'"); err == nil {
		t.Fatal("duplicate legacy lookup accepted")
	}
	var id, parent, unused int
	var detail string
	if err := database.QueryRow("EXPLAIN QUERY PLAN SELECT id FROM agent_keys WHERE legacy_lookup = ?", []byte{1}).
		Scan(&id, &parent, &unused, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "USING INDEX") || !strings.Contains(detail, "legacy_lookup") {
		t.Fatalf("legacy lookup does not use its index: %s", detail)
	}
	if err := database.QueryRow(`EXPLAIN QUERY PLAN
		SELECT id, name, key_hash, allowed_services, allowed_users, created_at FROM agent_keys
		WHERE key_id IS NULL AND legacy_lookup IS NULL AND revoked_at IS NULL`).
		Scan(&id, &parent, &unused, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "idx_agent_keys_legacy_pending") {
		t.Fatalf("fallback can scan historical rows: %s", detail)
	}
}
