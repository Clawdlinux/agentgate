package db

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
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
	database, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_busy_timeout=5000")
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
	for _, e := range entries {
		data, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("db.RunMigrations: read %s: %w", e.Name(), err)
		}
		if _, err := database.Exec(string(data)); err != nil {
			return fmt.Errorf("db.RunMigrations: exec %s: %w", e.Name(), err)
		}
	}
	return nil
}
