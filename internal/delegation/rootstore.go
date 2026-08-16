/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Clawdlinux/agentgate/internal/signer"
)

// purposeDelegationRootV1 domain-separates this store's derived storage key
// from every other package deriving from the same master key.
const purposeDelegationRootV1 = "agentgate.delegation.root.v1"

// ErrRootKeyUnreadable means the persisted root private key could not be
// decrypted with the supplied master key.
var ErrRootKeyUnreadable = errors.New("delegation: root key unreadable")

// RootStore persists AgentGate's single Biscuit trust root: one Ed25519
// keypair, generated once and never rotated. It mirrors internal/signer's
// AES-256-GCM encryption-at-rest approach, deriving its own purpose-
// specific key from the same master secret so no key material is shared
// across packages.
type RootStore struct {
	mu  sync.Mutex
	db  *sql.DB
	gcm cipher.AEAD
}

// NewRootStore creates a persistent Biscuit root key store. masterKey must
// be exactly 32 bytes, the same master secret internal/vault and
// internal/signer use.
func NewRootStore(db *sql.DB, masterKey []byte) (*RootStore, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("delegation: master key must be 32 bytes, got %d", len(masterKey))
	}
	storageKey := signer.DerivePurposeKey(masterKey, purposeDelegationRootV1)
	block, err := aes.NewCipher(storageKey)
	if err != nil {
		return nil, fmt.Errorf("delegation: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("delegation: new gcm: %w", err)
	}
	return &RootStore{db: db, gcm: gcm}, nil
}

func (s *RootStore) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("delegation: nonce: %w", err)
	}
	return s.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *RootStore) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("%w: ciphertext too short", ErrRootKeyUnreadable)
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRootKeyUnreadable, err)
	}
	return plaintext, nil
}

// LoadOrCreateRoot returns AgentGate's Biscuit trust root, generating and
// persisting one from crypto/rand on first call. Unlike internal/signer's
// receipt-signing keys, this root never rotates within this milestone: it
// is the single trust anchor every issued and verified Biscuit shares.
func (s *RootStore) LoadOrCreateRoot() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pub, encPriv []byte
	err := s.db.QueryRow(`SELECT public_key, private_key_enc FROM delegation_root_key WHERE id = 1`).Scan(&pub, &encPriv)
	if err == sql.ErrNoRows {
		return s.createLocked()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("delegation: query root: %w", err)
	}
	plaintext, err := s.decrypt(encPriv)
	if err != nil {
		return nil, nil, err
	}
	return ed25519.PublicKey(pub), ed25519.PrivateKey(plaintext), nil
}

func (s *RootStore) createLocked() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("delegation: generate key: %w", err)
	}
	encPriv, err := s.encrypt(priv)
	if err != nil {
		return nil, nil, err
	}
	_, err = s.db.Exec(`INSERT INTO delegation_root_key (id, public_key, private_key_enc) VALUES (1, ?, ?)`, []byte(pub), encPriv)
	if err != nil {
		return nil, nil, fmt.Errorf("delegation: insert root: %w", err)
	}
	return pub, priv, nil
}
