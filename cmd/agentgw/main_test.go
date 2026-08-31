package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/org"
)

func TestBootstrapAdmin(t *testing.T) {
	database, err := agentgatedb.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Setenv("AGENTGATE_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("AGENTGATE_BOOTSTRAP_ADMIN_PASSWORD", "correct horse battery staple")
	store := org.NewStore(database)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := bootstrapAdmin(context.Background(), store, logger); err != nil {
		t.Fatalf("bootstrapAdmin: %v", err)
	}
	if count, err := store.AdminCount(context.Background()); err != nil || count != 1 {
		t.Fatalf("AdminCount = (%d, %v), want (1, nil)", count, err)
	}
	if _, err := store.Authenticate(context.Background(), "admin@example.com", "correct horse battery staple"); err != nil {
		t.Fatalf("Authenticate bootstrap admin: %v", err)
	}
	if err := bootstrapAdmin(context.Background(), store, logger); err != nil {
		t.Fatalf("second bootstrapAdmin: %v", err)
	}
	if count, err := store.AdminCount(context.Background()); err != nil || count != 1 {
		t.Fatalf("AdminCount after second bootstrap = (%d, %v), want (1, nil)", count, err)
	}
}

func TestBootstrapAdminRequiresBothEnvironmentValues(t *testing.T) {
	database, err := agentgatedb.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Setenv("AGENTGATE_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("AGENTGATE_BOOTSTRAP_ADMIN_PASSWORD", "")

	if err := bootstrapAdmin(context.Background(), org.NewStore(database), slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("bootstrapAdmin succeeded with only email configured")
	}
}
