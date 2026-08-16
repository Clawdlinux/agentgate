/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"path/filepath"
	"testing"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
)

func testMasterKey() []byte {
	return []byte("01234567890123456789012345678901")
}

func TestRootStore_CreatesAndPersists(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "agentgate.db")
	database, err := agentgatedb.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("db.RunMigrations: %v", err)
	}

	store, err := NewRootStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("NewRootStore: %v", err)
	}

	pub1, priv1, err := store.LoadOrCreateRoot()
	if err != nil {
		t.Fatalf("LoadOrCreateRoot (create): %v", err)
	}
	if len(pub1) == 0 || len(priv1) == 0 {
		t.Fatal("expected non-empty generated keypair")
	}

	pub2, priv2, err := store.LoadOrCreateRoot()
	if err != nil {
		t.Fatalf("LoadOrCreateRoot (reload): %v", err)
	}
	if string(pub1) != string(pub2) || string(priv1) != string(priv2) {
		t.Fatal("a second call returned a different keypair instead of the persisted one")
	}
}

func TestRootStore_PersistsAcrossInstances(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "agentgate.db")
	database, err := agentgatedb.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("db.RunMigrations: %v", err)
	}

	store1, err := NewRootStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("NewRootStore (1st): %v", err)
	}
	pub1, _, err := store1.LoadOrCreateRoot()
	if err != nil {
		t.Fatalf("LoadOrCreateRoot (1st): %v", err)
	}

	store2, err := NewRootStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("NewRootStore (2nd, fresh instance): %v", err)
	}
	pub2, _, err := store2.LoadOrCreateRoot()
	if err != nil {
		t.Fatalf("LoadOrCreateRoot (2nd instance): %v", err)
	}
	if string(pub1) != string(pub2) {
		t.Fatal("a fresh RootStore instance over the same database generated a new root instead of loading the persisted one")
	}
}

func TestRootStore_RejectsShortMasterKey(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "agentgate.db")
	database, err := agentgatedb.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	if _, err := NewRootStore(database, []byte("too-short")); err == nil {
		t.Fatal("expected a non-32-byte master key to be rejected")
	}
}
