package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionManagerAuthenticate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	manager := NewSessionManager([]byte("dev-key-change-in-production-32b"), "https://agentgate.example")
	manager.now = func() time.Time { return now }
	cookie, err := manager.CreateCookie(SessionIdentity{AdminID: "admin-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("CreateCookie: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	request.AddCookie(cookie)

	identity, err := manager.Authenticate(request)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity != (SessionIdentity{AdminID: "admin-1", OrgID: "org-1"}) {
		t.Fatalf("identity = %#v", identity)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie has unsafe attributes: %#v", cookie)
	}
}

func TestSessionManagerRejectsInvalidSessions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	manager := NewSessionManager([]byte("dev-key-change-in-production-32b"), "http://localhost:8080")
	manager.now = func() time.Time { return now }
	validCookie, err := manager.CreateCookie(SessionIdentity{AdminID: "admin-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("CreateCookie: %v", err)
	}
	expiredManager := NewSessionManager([]byte("dev-key-change-in-production-32b"), "http://localhost:8080")
	expiredManager.now = func() time.Time { return now.Add(2 * sessionTTL) }
	wrongKeyManager := NewSessionManager([]byte("other-key-change-in-production-32"), "http://localhost:8080")

	tests := []struct {
		name    string
		cookie  *http.Cookie
		manager *SessionManager
	}{
		{name: "missing cookie", manager: manager},
		{name: "malformed value", cookie: &http.Cookie{Name: sessionCookieName, Value: "not base64"}, manager: manager},
		{name: "tampered value", cookie: &http.Cookie{Name: sessionCookieName, Value: validCookie.Value[:len(validCookie.Value)-1] + "A"}, manager: manager},
		{name: "expired session", cookie: validCookie, manager: expiredManager},
		{name: "wrong session key", cookie: validCookie, manager: wrongKeyManager},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
			if test.cookie != nil {
				request.AddCookie(test.cookie)
			}
			if _, err := test.manager.Authenticate(request); err != ErrInvalidSession {
				t.Fatalf("Authenticate error = %v, want %v", err, ErrInvalidSession)
			}
		})
	}
}

func TestSessionManagerCookieSecurityFollowsPublicURL(t *testing.T) {
	t.Parallel()
	manager := NewSessionManager([]byte("dev-key-change-in-production-32b"), "http://localhost:8080")
	cookie, err := manager.CreateCookie(SessionIdentity{AdminID: "admin-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("CreateCookie: %v", err)
	}
	if cookie.Secure {
		t.Fatal("http public URL set Secure cookie")
	}
	cleared := manager.ClearCookie()
	if cleared.MaxAge != -1 || !cleared.HttpOnly || cleared.SameSite != http.SameSiteLaxMode {
		t.Fatalf("ClearCookie = %#v", cleared)
	}
}
