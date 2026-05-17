package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrKeyNotFound = errors.New("auth: key not found")
	ErrKeyRevoked  = errors.New("auth: key revoked")
)

// AgentKey represents an API key for an agent.
type AgentKey struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	KeyHash         string     `json:"-"`
	AllowedServices []string   `json:"allowed_services"`
	AllowedUsers    []string   `json:"allowed_users"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

// CanAccessService checks if the key is allowed to access the given service.
func (k *AgentKey) CanAccessService(service string) bool {
	for _, s := range k.AllowedServices {
		if s == "*" || s == service {
			return true
		}
	}
	return false
}

// CanAccessUser checks if the key is allowed to act on behalf of the given user.
func (k *AgentKey) CanAccessUser(userID string) bool {
	for _, u := range k.AllowedUsers {
		if u == "*" || u == userID {
			return true
		}
	}
	return false
}

// KeyStore manages agent API keys in SQLite.
type KeyStore struct {
	db *sql.DB
}

// NewKeyStore creates a new key store backed by SQLite.
func NewKeyStore(db *sql.DB) *KeyStore {
	return &KeyStore{db: db}
}

// GenerateKey creates a new random API key.
// Returns the plaintext key (shown once to user) and the bcrypt hash (stored).
func GenerateKey() (plaintext string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("auth: generate key: %w", err)
	}
	plaintext = "ag_live_" + hex.EncodeToString(raw)

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("auth: hash key: %w", err)
	}
	return plaintext, string(hashBytes), nil
}

// Create creates a new agent key and stores it.
// Returns the AgentKey and the plaintext key (shown once).
func (s *KeyStore) Create(ctx context.Context, name string, services, users []string) (*AgentKey, string, error) {
	plaintext, hash, err := GenerateKey()
	if err != nil {
		return nil, "", err
	}

	id := generateID()
	servicesJSON, _ := json.Marshal(services)
	usersJSON, _ := json.Marshal(users)

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO agent_keys (id, name, key_hash, allowed_services, allowed_users) VALUES (?, ?, ?, ?, ?)",
		id, name, hash, string(servicesJSON), string(usersJSON),
	)
	if err != nil {
		return nil, "", fmt.Errorf("auth: create key: %w", err)
	}

	key := &AgentKey{
		ID:              id,
		Name:            name,
		AllowedServices: services,
		AllowedUsers:    users,
		CreatedAt:       time.Now(),
	}
	return key, plaintext, nil
}

// Validate checks a plaintext API key against stored hashes.
// Returns the matching AgentKey or an error.
func (s *KeyStore) Validate(ctx context.Context, plaintext string) (*AgentKey, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, key_hash, allowed_services, allowed_users, created_at, revoked_at FROM agent_keys",
	)
	if err != nil {
		return nil, fmt.Errorf("auth: query keys: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key AgentKey
		var servicesJSON, usersJSON string
		var revokedAt sql.NullTime

		if err := rows.Scan(&key.ID, &key.Name, &key.KeyHash, &servicesJSON, &usersJSON, &key.CreatedAt, &revokedAt); err != nil {
			continue
		}

		if bcrypt.CompareHashAndPassword([]byte(key.KeyHash), []byte(plaintext)) != nil {
			continue
		}

		// Found matching key.
		if revokedAt.Valid {
			key.RevokedAt = &revokedAt.Time
			return nil, ErrKeyRevoked
		}

		json.Unmarshal([]byte(servicesJSON), &key.AllowedServices)
		json.Unmarshal([]byte(usersJSON), &key.AllowedUsers)
		return &key, nil
	}

	return nil, ErrKeyNotFound
}

// Revoke marks a key as revoked.
func (s *KeyStore) Revoke(ctx context.Context, keyID string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE agent_keys SET revoked_at = CURRENT_TIMESTAMP WHERE id = ? AND revoked_at IS NULL",
		keyID,
	)
	if err != nil {
		return fmt.Errorf("auth: revoke: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrKeyNotFound
	}
	return nil
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
