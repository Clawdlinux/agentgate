package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
)

// SQLiteStore is a persistent Store backed by SQLite with AES-256-GCM encryption.
type SQLiteStore struct {
	db  *sql.DB
	gcm cipher.AEAD
}

// NewSQLiteStore creates a persistent encrypted store.
// The encryption key must be exactly 32 bytes.
func NewSQLiteStore(db *sql.DB, encryptionKey []byte) (*SQLiteStore, error) {
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
	return &SQLiteStore{db: db, gcm: gcm}, nil
}

func (s *SQLiteStore) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("vault: nonce: %w", err)
	}
	return s.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *SQLiteStore) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := s.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("vault: ciphertext too short")
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return s.gcm.Open(nil, nonce, data, nil)
}

func (s *SQLiteStore) Put(userID, service string, tok Token) error {
	accessEnc, err := s.encrypt([]byte(tok.AccessToken))
	if err != nil {
		return fmt.Errorf("vault.Put: encrypt access: %w", err)
	}

	var refreshEnc []byte
	if tok.RefreshToken != "" {
		refreshEnc, err = s.encrypt([]byte(tok.RefreshToken))
		if err != nil {
			return fmt.Errorf("vault.Put: encrypt refresh: %w", err)
		}
	}

	scopesJSON, _ := json.Marshal(tok.Scopes)

	_, err = s.db.Exec(`
		INSERT INTO tokens (user_id, service, access_token_enc, refresh_token_enc, expires_at, scopes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, service) DO UPDATE SET
			access_token_enc = excluded.access_token_enc,
			refresh_token_enc = excluded.refresh_token_enc,
			expires_at = excluded.expires_at,
			scopes = excluded.scopes,
			updated_at = CURRENT_TIMESTAMP
	`, userID, service, accessEnc, refreshEnc, tok.ExpiresAt, string(scopesJSON))
	if err != nil {
		return fmt.Errorf("vault.Put: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Get(userID, service string) (Token, error) {
	var accessEnc, refreshEnc []byte
	var expiresAt sql.NullTime
	var scopesJSON string

	err := s.db.QueryRow(`
		SELECT access_token_enc, refresh_token_enc, expires_at, scopes
		FROM tokens WHERE user_id = ? AND service = ?
	`, userID, service).Scan(&accessEnc, &refreshEnc, &expiresAt, &scopesJSON)
	if err == sql.ErrNoRows {
		return Token{}, fmt.Errorf("%w: %s:%s", ErrTokenNotFound, userID, service)
	}
	if err != nil {
		return Token{}, fmt.Errorf("vault.Get: %w", err)
	}

	accessPlain, err := s.decrypt(accessEnc)
	if err != nil {
		return Token{}, fmt.Errorf("vault.Get: decrypt access: %w", err)
	}

	tok := Token{
		AccessToken: string(accessPlain),
	}

	if len(refreshEnc) > 0 {
		refreshPlain, err := s.decrypt(refreshEnc)
		if err != nil {
			return Token{}, fmt.Errorf("vault.Get: decrypt refresh: %w", err)
		}
		tok.RefreshToken = string(refreshPlain)
	}

	if expiresAt.Valid {
		tok.ExpiresAt = expiresAt.Time
	}

	if scopesJSON != "" {
		json.Unmarshal([]byte(scopesJSON), &tok.Scopes)
	}

	return tok, nil
}

func (s *SQLiteStore) Delete(userID, service string) error {
	_, err := s.db.Exec("DELETE FROM tokens WHERE user_id = ? AND service = ?", userID, service)
	if err != nil {
		return fmt.Errorf("vault.Delete: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListServices(userID string) ([]string, error) {
	rows, err := s.db.Query("SELECT service FROM tokens WHERE user_id = ?", userID)
	if err != nil {
		return nil, fmt.Errorf("vault.ListServices: %w", err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, fmt.Errorf("vault.ListServices: scan: %w", err)
		}
		services = append(services, svc)
	}
	return services, rows.Err()
}
