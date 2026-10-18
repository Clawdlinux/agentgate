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
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
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
	tests := []struct {
		name       string
		publicURL  string
		wantSecure bool
	}{
		{name: "https", publicURL: "https://agentgate.example", wantSecure: true},
		{name: "uppercase https", publicURL: "HTTPS://agentgate.example", wantSecure: true},
		{name: "https localhost", publicURL: "https://localhost:8080", wantSecure: true},
		{name: "http localhost", publicURL: "http://localhost:8080"},
		{name: "http uppercase localhost", publicURL: "http://LOCALHOST:8080"},
		{name: "http loopback", publicURL: "http://127.0.0.1:8080"},
		{name: "http remote", publicURL: "http://agentgate.example", wantSecure: true},
		{name: "http lookalike", publicURL: "http://localhost.example", wantSecure: true},
		{name: "http userinfo", publicURL: "http://remote@localhost", wantSecure: true},
		{name: "http ipv6", publicURL: "http://[::1]:8080", wantSecure: true},
		{name: "missing url", wantSecure: true},
		{name: "relative url", publicURL: "/localhost", wantSecure: true},
		{name: "malformed url", publicURL: "http://localhost:%", wantSecure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
			manager := NewSessionManager([]byte("dev-key-change-in-production-32b"), test.publicURL)
			manager.now = func() time.Time { return now }
			cookie, err := manager.CreateCookie(SessionIdentity{AdminID: "admin-1", OrgID: "org-1"})
			if err != nil {
				t.Fatalf("CreateCookie: %v", err)
			}
			if cookie.Name != sessionCookieName || cookie.Value == "" ||
				cookie.MaxAge != int(sessionTTL.Seconds()) || !cookie.Expires.Equal(now.Add(sessionTTL)) {
				t.Fatalf("CreateCookie = %#v", cookie)
			}
			cleared := manager.ClearCookie()
			if cleared.Name != sessionCookieName || cleared.Value != "" || cleared.MaxAge != -1 {
				t.Fatalf("ClearCookie = %#v", cleared)
			}
			for _, c := range []*http.Cookie{cookie, cleared} {
				if c.Secure != test.wantSecure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
					t.Fatalf("cookie has incorrect attributes: %#v", c)
				}
			}
		})
	}
}
