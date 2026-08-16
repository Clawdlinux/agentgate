/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package vault stores and retrieves OAuth tokens (access + refresh) per
// user × service pair. Tokens are encrypted at rest using AES-256-GCM.
//
// The MVP uses an in-memory store with an encryption interface. Production
// deployments should swap in a persistent backend (Postgres, Vault, etc.)
// behind the same Store interface.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// ErrTokenNotFound is returned when no token exists for a user × service pair.
var ErrTokenNotFound = errors.New("vault: token not found")

// Token holds an OAuth token set for one user × one service.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// IsExpired returns true if the access token has expired.
func (t Token) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// Store is the interface for token persistence. Implementations must be
// goroutine-safe.
type Store interface {
	Put(userID, service string, tok Token) error
	Get(userID, service string) (Token, error)
	Delete(userID, service string) error
	ListServices(userID string) ([]string, error)
}

// MemoryStore is an in-memory Store with AES-256-GCM encryption at rest.
// Suitable for development and testing. Production should use a persistent
// backend.
type MemoryStore struct {
	mu     sync.RWMutex
	tokens map[string][]byte // key: "userID:service" -> encrypted JSON
	gcm    cipher.AEAD
}

// NewMemoryStore creates an encrypted in-memory store. The key must be
// exactly 32 bytes (AES-256).
func NewMemoryStore(encryptionKey []byte) (*MemoryStore, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("vault: encryption key must be 32 bytes, got %d", len(encryptionKey))
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("vault: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: new gcm: %w", err)
	}
	return &MemoryStore{
		tokens: make(map[string][]byte),
		gcm:    gcm,
	}, nil
}

func storeKey(userID, service string) string {
	return userID + ":" + service
}

func (s *MemoryStore) Put(userID, service string, tok Token) error {
	plaintext, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("vault: marshal: %w", err)
	}

	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("vault: nonce: %w", err)
	}

	ciphertext := s.gcm.Seal(nonce, nonce, plaintext, nil)

	s.mu.Lock()
	s.tokens[storeKey(userID, service)] = ciphertext
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) Get(userID, service string) (Token, error) {
	s.mu.RLock()
	ciphertext, ok := s.tokens[storeKey(userID, service)]
	s.mu.RUnlock()

	if !ok {
		return Token{}, fmt.Errorf("%w: %s:%s", ErrTokenNotFound, userID, service)
	}

	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return Token{}, errors.New("vault: ciphertext too short")
	}

	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := s.gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return Token{}, fmt.Errorf("vault: decrypt: %w", err)
	}

	var tok Token
	if err := json.Unmarshal(plaintext, &tok); err != nil {
		return Token{}, fmt.Errorf("vault: unmarshal: %w", err)
	}
	return tok, nil
}

func (s *MemoryStore) Delete(userID, service string) error {
	s.mu.Lock()
	delete(s.tokens, storeKey(userID, service))
	s.mu.Unlock()
	return nil
}

func (s *MemoryStore) ListServices(userID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := userID + ":"
	var services []string
	for k := range s.tokens {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			services = append(services, k[len(prefix):])
		}
	}
	return services, nil
}
