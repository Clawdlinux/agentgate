/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package signer

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func testMasterKey() []byte {
	return []byte("01234567890123456789012345678901")
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE signer_keys (
			kid TEXT PRIMARY KEY,
			public_key BLOB NOT NULL,
			private_key_enc BLOB NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			valid_from_seq INTEGER NOT NULL,
			valid_until_seq INTEGER,
			active INTEGER NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX idx_signer_keys_one_active ON signer_keys(active) WHERE active = 1;
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// KEY-01: first persistent startup generates one Ed25519 keypair from
// cryptographic randomness.
func TestLoadOrCreateActive_GeneratesOnFirstStartup(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}

	record, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("public key size = %d, want %d", len(record.PublicKey), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("private key size = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
	if record.ValidFromSeq != 1 {
		t.Fatalf("valid_from_seq = %d, want 1", record.ValidFromSeq)
	}
	if record.ValidUntilSeq != nil {
		t.Fatalf("valid_until_seq = %v, want nil (open-ended)", record.ValidUntilSeq)
	}
	if !ed25519.PublicKey(record.PublicKey).Equal(priv.Public().(ed25519.PublicKey)) {
		t.Fatal("returned public key does not match the returned private key")
	}
}

// KEY-03: restart preserves signer identity.
func TestLoadOrCreateActive_RestartPreservesIdentity(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}

	first, firstPriv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate restart: a fresh Store over the same db and master key.
	restarted, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	second, secondPriv, err := restarted.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}

	if first.KID != second.KID {
		t.Fatalf("kid changed across restart: %s != %s", first.KID, second.KID)
	}
	if string(firstPriv) != string(secondPriv) {
		t.Fatal("private key changed across restart")
	}
}

// KEY-03: an unreadable persistent key must stop startup, not be replaced silently.
func TestLoadOrCreateActive_UnreadableKeyFailsClosed(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}

	wrongKey := []byte("98765432109876543210987654321098")
	wrongStore, err := NewStore(db, wrongKey)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = wrongStore.LoadOrCreateActive(1)
	if err == nil {
		t.Fatal("expected ErrKeyUnreadable, got nil")
	}
	if !errors.Is(err, ErrKeyUnreadable) {
		t.Fatalf("expected ErrKeyUnreadable, got %v", err)
	}

	// The original key must still be exactly as it was — no silent replacement.
	rows, err := db.Query(`SELECT kid FROM signer_keys`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var count int
	var onlyKID string
	for rows.Next() {
		count++
		if err := rows.Scan(&onlyKID); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1 (no replacement row inserted)", count)
	}
	if onlyKID != original.KID {
		t.Fatalf("kid = %s, want unchanged %s", onlyKID, original.KID)
	}
}

// KEY-05: deterministic key ID bound to the public key bytes.
func TestComputeKID_DeterministicAndBoundToPublicKey(t *testing.T) {
	t.Parallel()
	pub1, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub2, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	if ComputeKID(pub1) != ComputeKID(pub1) {
		t.Fatal("ComputeKID is not deterministic for the same public key")
	}
	if ComputeKID(pub1) == ComputeKID(pub2) {
		t.Fatal("ComputeKID collided for two different public keys")
	}
	if !strings.HasPrefix(ComputeKID(pub1), kidPrefix) {
		t.Fatalf("kid missing prefix %q: %s", kidPrefix, ComputeKID(pub1))
	}
	if len(ComputeKID(pub1)) > 128 {
		t.Fatalf("kid length %d exceeds Receipt.SignerKID's 128-byte limit", len(ComputeKID(pub1)))
	}
}

// KEY-06: rotation preserves verification of old receipts by binding each
// key to its own valid sequence interval, and KEY-04: PublicKeys exposes
// active and historical keys with no private material.
func TestRotate_PreservesOldKeyForVerification(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}

	first, firstPriv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}

	rotated, rotatedPriv, err := store.Rotate(100)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.KID == first.KID {
		t.Fatal("rotation did not produce a new key")
	}
	if string(rotatedPriv) == string(firstPriv) {
		t.Fatal("rotated private key matches the original — not a fresh key")
	}
	if rotated.ValidFromSeq != 100 {
		t.Fatalf("rotated valid_from_seq = %d, want 100", rotated.ValidFromSeq)
	}

	records, err := store.PublicKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("public key count = %d, want 2 (old + new)", len(records))
	}

	var oldRecord, newRecord KeyRecord
	for _, record := range records {
		if record.KID == first.KID {
			oldRecord = record
		}
		if record.KID == rotated.KID {
			newRecord = record
		}
	}
	if oldRecord.ValidUntilSeq == nil || *oldRecord.ValidUntilSeq != 99 {
		t.Fatalf("old key valid_until_seq = %v, want 99", oldRecord.ValidUntilSeq)
	}
	if newRecord.ValidUntilSeq != nil {
		t.Fatalf("new key valid_until_seq = %v, want nil (still active)", newRecord.ValidUntilSeq)
	}

	// A receipt signed at seq 50 (under the old key) must still verify
	// against the old key's public bytes — that's what "verification of old
	// receipts" means here.
	message := []byte("receipt-seq-50")
	signature := Sign(firstPriv, message)
	if !Verify(oldRecord.PublicKey, message, signature) {
		t.Fatal("old key can no longer verify a message it signed before rotation")
	}
}

func TestRotate_RejectsNonIncreasingSequence(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadOrCreateActive(10); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Rotate(10); err == nil {
		t.Fatal("expected ErrRotationSequence for atSeq == valid_from_seq")
	}
	if _, _, err := store.Rotate(5); err == nil {
		t.Fatal("expected ErrRotationSequence for atSeq < valid_from_seq")
	}
}

// KEY-07: an independent verifier needs only a public key and history —
// Verify and PublicKeys never require the private store.
func TestVerify_RequiresOnlyPublicKey(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("independent-verification")
	signature := Sign(priv, message)

	if !Verify(pub, message, signature) {
		t.Fatal("Verify rejected a valid signature")
	}
	tampered := append([]byte(nil), message...)
	tampered[0] ^= 0xFF
	if Verify(pub, tampered, signature) {
		t.Fatal("Verify accepted a signature over a different message")
	}
}
