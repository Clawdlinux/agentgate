package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Clawdlinux/agentgate/internal/signer"
)

const (
	sessionCookieName = "agentgate_admin_session"
	sessionTTL        = time.Hour
	sessionKeyPurpose = "agentgate.admin-session.v1"
)

var ErrInvalidSession = errors.New("admin: invalid session")

type SessionIdentity struct {
	AdminID string
	OrgID   string
}

type sessionPayload struct {
	AdminID   string    `json:"a"`
	OrgID     string    `json:"o"`
	ExpiresAt time.Time `json:"e"`
}

type SessionManager struct {
	key    []byte
	secure bool
	now    func() time.Time
}

func NewSessionManager(masterKey []byte, publicURL string) *SessionManager {
	return &SessionManager{
		key:    signer.DerivePurposeKey(masterKey, sessionKeyPurpose),
		secure: strings.HasPrefix(publicURL, "https://"),
		now:    time.Now,
	}
}

func (m *SessionManager) CreateCookie(identity SessionIdentity) (*http.Cookie, error) {
	if identity.AdminID == "" || identity.OrgID == "" {
		return nil, ErrInvalidSession
	}
	payload, err := json.Marshal(sessionPayload{
		AdminID:   identity.AdminID,
		OrgID:     identity.OrgID,
		ExpiresAt: m.now().Add(sessionTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("admin.CreateCookie: marshal payload: %w", err)
	}

	block, err := aes.NewCipher(m.key)
	if err != nil {
		return nil, fmt.Errorf("admin.CreateCookie: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("admin.CreateCookie: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("admin.CreateCookie: nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, payload, nil)
	expiresAt := m.now().Add(sessionTTL)
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    base64.URLEncoding.EncodeToString(ciphertext),
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

func (m *SessionManager) Authenticate(r *http.Request) (SessionIdentity, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return SessionIdentity{}, ErrInvalidSession
	}

	ciphertext, err := base64.URLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return SessionIdentity{}, ErrInvalidSession
	}
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return SessionIdentity{}, fmt.Errorf("admin.Authenticate: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return SessionIdentity{}, fmt.Errorf("admin.Authenticate: gcm: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return SessionIdentity{}, ErrInvalidSession
	}
	plaintext, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		return SessionIdentity{}, ErrInvalidSession
	}

	var payload sessionPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return SessionIdentity{}, ErrInvalidSession
	}
	if payload.AdminID == "" || payload.OrgID == "" || !m.now().Before(payload.ExpiresAt) {
		return SessionIdentity{}, ErrInvalidSession
	}
	return SessionIdentity{AdminID: payload.AdminID, OrgID: payload.OrgID}, nil
}

func (m *SessionManager) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	}
}
