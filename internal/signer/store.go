/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package signer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"sync"
	"time"
)

// Store persists signer keys in SQLite with AES-256-GCM encryption of the
// private key material. It mirrors internal/vault's encryption approach so
// the two packages stay auditable against the same pattern.
type Store struct {
	mu  sync.Mutex
	db  *sql.DB
	gcm cipher.AEAD
}

// NewStore creates a persistent signer key store. masterKey must be exactly
// 32 bytes — the same master secret internal/vault uses. The store derives
// its own purpose-specific encryption key from it and never uses masterKey
// directly for encryption.
func NewStore(db *sql.DB, masterKey []byte) (*Store, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("signer: master key must be 32 bytes, got %d", len(masterKey))
	}
	storageKey := DerivePurposeKey(masterKey, purposeSignerV1)
	block, err := aes.NewCipher(storageKey)
	if err != nil {
		return nil, fmt.Errorf("signer: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("signer: new gcm: %w", err)
	}
	return &Store{db: db, gcm: gcm}, nil
}

func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("signer: nonce: %w", err)
	}
	return s.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Store) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("%w: ciphertext too short", ErrKeyUnreadable)
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyUnreadable, err)
	}
	return plaintext, nil
}

// LoadOrCreateActive returns the active signing key and its private
// material. If no active key exists, it generates one from crypto/rand and
// persists it with valid_from_seq = firstSeq. If an active key exists but
// its private material cannot be decrypted, it returns ErrKeyUnreadable —
// it never generates a replacement in that case, because that would break
// verification of every receipt already signed under the original key.
func (s *Store) LoadOrCreateActive(firstSeq uint64) (KeyRecord, ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, encPriv, err := s.queryActive()
	if err == sql.ErrNoRows {
		return s.createActiveLocked(firstSeq)
	}
	if err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: query active: %w", err)
	}

	plaintext, err := s.decrypt(encPriv)
	if err != nil {
		return KeyRecord{}, nil, err
	}
	return record, ed25519.PrivateKey(plaintext), nil
}

// Rotate deactivates the current active key at atSeq-1 and activates a new
// key starting at atSeq. atSeq must strictly exceed the current active
// key's valid_from_seq.
func (s *Store) Rotate(atSeq uint64) (KeyRecord, ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, _, err := s.queryActive()
	if err == sql.ErrNoRows {
		return s.createActiveLocked(atSeq)
	}
	if err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: query active: %w", err)
	}
	if atSeq <= current.ValidFromSeq {
		return KeyRecord{}, nil, ErrRotationSequence
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: generate key: %w", err)
	}
	encPriv, err := s.encrypt(priv)
	if err != nil {
		return KeyRecord{}, nil, err
	}
	kid := ComputeKID(pub)
	untilSeq := atSeq - 1

	tx, err := s.db.Begin()
	if err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE signer_keys SET active = 0, valid_until_seq = ? WHERE kid = ?`, untilSeq, current.KID); err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: deactivate current: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO signer_keys (kid, public_key, private_key_enc, valid_from_seq, valid_until_seq, active)
		VALUES (?, ?, ?, ?, NULL, 1)
	`, kid, []byte(pub), encPriv, atSeq); err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: insert new active: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: commit rotate: %w", err)
	}

	return KeyRecord{
		KID:          kid,
		PublicKey:    pub,
		ValidFromSeq: atSeq,
	}, priv, nil
}

// PublicKeys returns every signing key, active and historical, ordered by
// valid_from_seq ascending. Returned records contain no private material.
func (s *Store) PublicKeys() ([]KeyRecord, error) {
	rows, err := s.db.Query(`
		SELECT kid, public_key, created_at, valid_from_seq, valid_until_seq
		FROM signer_keys ORDER BY valid_from_seq ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("signer: query public keys: %w", err)
	}
	defer rows.Close()

	var records []KeyRecord
	for rows.Next() {
		record, err := scanKeyRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) queryActive() (KeyRecord, []byte, error) {
	row := s.db.QueryRow(`
		SELECT kid, public_key, private_key_enc, created_at, valid_from_seq, valid_until_seq
		FROM signer_keys WHERE active = 1
	`)
	var record KeyRecord
	var encPriv []byte
	var createdAt time.Time
	var untilSeq sql.NullInt64
	if err := row.Scan(&record.KID, (*sqliteBlob)(&record.PublicKey), &encPriv, &createdAt, &record.ValidFromSeq, &untilSeq); err != nil {
		return KeyRecord{}, nil, err
	}
	record.CreatedAt = createdAt
	if untilSeq.Valid {
		value := uint64(untilSeq.Int64)
		record.ValidUntilSeq = &value
	}
	return record, encPriv, nil
}

func (s *Store) createActiveLocked(firstSeq uint64) (KeyRecord, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: generate key: %w", err)
	}
	encPriv, err := s.encrypt(priv)
	if err != nil {
		return KeyRecord{}, nil, err
	}
	kid := ComputeKID(pub)

	_, err = s.db.Exec(`
		INSERT INTO signer_keys (kid, public_key, private_key_enc, valid_from_seq, valid_until_seq, active)
		VALUES (?, ?, ?, ?, NULL, 1)
	`, kid, []byte(pub), encPriv, firstSeq)
	if err != nil {
		return KeyRecord{}, nil, fmt.Errorf("signer: insert first active: %w", err)
	}

	return KeyRecord{
		KID:          kid,
		PublicKey:    pub,
		ValidFromSeq: firstSeq,
	}, priv, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanKeyRecord(row rowScanner) (KeyRecord, error) {
	var record KeyRecord
	var createdAt time.Time
	var untilSeq sql.NullInt64
	if err := row.Scan(&record.KID, (*sqliteBlob)(&record.PublicKey), &createdAt, &record.ValidFromSeq, &untilSeq); err != nil {
		return KeyRecord{}, fmt.Errorf("signer: scan key record: %w", err)
	}
	record.CreatedAt = createdAt
	if untilSeq.Valid {
		value := uint64(untilSeq.Int64)
		record.ValidUntilSeq = &value
	}
	return record, nil
}

// sqliteBlob adapts a BLOB column into ed25519.PublicKey via database/sql's
// Scanner interface, since ed25519.PublicKey is []byte under the hood.
type sqliteBlob ed25519.PublicKey

func (b *sqliteBlob) Scan(src any) error {
	data, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("signer: expected []byte column, got %T", src)
	}
	*b = make(sqliteBlob, len(data))
	copy(*b, data)
	return nil
}
