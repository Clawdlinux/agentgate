package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type statePayload struct {
	UserID    string    `json:"u"`
	Service   string    `json:"s"`
	ExpiresAt time.Time `json:"e"`
}

// EncryptState creates an encrypted, time-limited OAuth state parameter.
// The state expires after 10 minutes.
func EncryptState(userID, service string, key []byte) (string, error) {
	payload := statePayload{
		UserID:    userID,
		Service:   service,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("oauth: marshal state: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("oauth: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("oauth: gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("oauth: nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// DecryptState decrypts and validates an OAuth state parameter.
// Returns an error if the state is expired or tampered.
func DecryptState(state string, key []byte) (userID, service string, err error) {
	ciphertext, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return "", "", fmt.Errorf("oauth: decode state: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", fmt.Errorf("oauth: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", fmt.Errorf("oauth: gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", "", fmt.Errorf("oauth: state too short")
	}

	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", "", fmt.Errorf("oauth: decrypt state: %w", err)
	}

	var payload statePayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", "", fmt.Errorf("oauth: unmarshal state: %w", err)
	}

	if time.Now().After(payload.ExpiresAt) {
		return "", "", fmt.Errorf("oauth: state expired")
	}

	return payload.UserID, payload.Service, nil
}
