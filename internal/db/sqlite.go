package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Open(dsn string) (*sql.DB, error) {
	dir := filepath.Dir(dsn)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("db.Open: mkdir %s: %w", dir, err)
		}
	}
	database, err := sql.Open("sqlite", dsn+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("db.Open: %w", err)
	}
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, fmt.Errorf("db.Open: ping: %w", err)
	}
	return database, nil
}

func RunMigrations(database *sql.DB) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("db.RunMigrations: read dir: %w", err)
	}
	// Track applied files because ALTER TABLE migrations cannot be replayed.
	if _, err := database.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)"); err != nil {
		return fmt.Errorf("db.RunMigrations: create history: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("db.RunMigrations: read %s: %w", e.Name(), err)
		}
		if err := applyMigration(database, e.Name(), string(data)); err != nil {
			return fmt.Errorf("db.RunMigrations: exec %s: %w", e.Name(), err)
		}
	}
	return nil
}

func applyMigration(database *sql.DB, name, data string) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applied int
	if err := tx.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&applied); err != nil {
		return err
	}
	if applied != 0 {
		return tx.Commit()
	}
	if _, err := tx.Exec(data); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (name) VALUES (?)", name); err != nil {
		return err
	}
	return tx.Commit()
}
