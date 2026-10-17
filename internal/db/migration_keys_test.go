package db

import (
	"database/sql"
	"strings"
	"testing"
)

func TestKeyLookupMigrationUpgradesLegacyAndIsRepeatable(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	defer database.Close()
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "005") {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(string(data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec("INSERT INTO agent_keys (id, name, key_hash) VALUES ('legacy', 'agent', 'old-bcrypt')"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := RunMigrations(database); err != nil {
			t.Fatal(err)
		}
	}
	var hash string
	var id sql.NullString
	var mac []byte
	if err := database.QueryRow("SELECT key_hash, key_id, key_hmac FROM agent_keys WHERE id = 'legacy'").
		Scan(&hash, &id, &mac); err != nil {
		t.Fatal(err)
	}
	if hash != "old-bcrypt" || id.Valid || mac != nil {
		t.Fatal("migration changed the legacy key")
	}
	if _, err := database.Exec("INSERT INTO agent_keys (id, name, key_hash) VALUES ('legacy2', 'agent', 'old-bcrypt')"); err != nil {
		t.Fatalf("multiple NULL key IDs must be allowed: %v", err)
	}
	if _, err := database.Exec("INSERT INTO agent_keys (id, name, key_hash, key_id) VALUES ('new', 'agent', '', 'lookup')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO agent_keys (id, name, key_hash, key_id) VALUES ('duplicate', 'agent', '', 'lookup')"); err == nil {
		t.Fatal("duplicate key ID was accepted")
	}
	var planID, parent, unused int
	var detail string
	if err := database.QueryRow("EXPLAIN QUERY PLAN SELECT id FROM agent_keys WHERE key_id = ?", "lookup").
		Scan(&planID, &parent, &unused, &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "USING INDEX") || !strings.Contains(detail, "key_id") {
		t.Fatalf("lookup does not use the key ID index: %s", detail)
	}
}

func TestMigrationFailureRollsBackSchemaAndHistory(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	if err := RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	const name = "test_transaction.sql"
	if err := applyMigration(database, name,
		"ALTER TABLE agent_keys ADD COLUMN rollback_probe TEXT; INVALID SQL"); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var applied int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatal("failed migration was recorded as applied")
	}
	if err := applyMigration(database, name, "ALTER TABLE agent_keys ADD COLUMN rollback_probe TEXT"); err != nil {
		t.Fatalf("schema change was not rolled back: %v", err)
	}
	if err := applyMigration(database, name, "INVALID SQL"); err != nil {
		t.Fatalf("applied migration was replayed: %v", err)
	}
}
