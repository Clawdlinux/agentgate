package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/org"
)

func testHandler(t *testing.T) (*Handler, SessionIdentity) {
	t.Helper()
	database, err := agentgatedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	orgStore := org.NewStore(database)
	organization, err := orgStore.CreateOrg(t.Context(), "Example Inc.")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	admin, err := orgStore.CreateAdmin(t.Context(), organization.ID, "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	return NewHandler(nil, nil, nil, nil, orgStore, NewSessionManager([]byte("dev-key-change-in-production-32b"), "https://agentgate.example"), "admin-secret", nil), SessionIdentity{AdminID: admin.ID, OrgID: organization.ID}
}

func TestHandlerLoginSetsSessionCookie(t *testing.T) {
	t.Parallel()
	handler, identity := testHandler(t)
	form := url.Values{"email": {"admin@example.com"}, "password": {"correct horse battery staple"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBufferString(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.Login(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("Login status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("Login cookies = %#v", cookies)
	}
	protected := handler.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual, ok := AdminFromContext(r.Context())
		if !ok || actual != identity {
			t.Fatalf("AdminFromContext = (%#v, %t), want (%#v, true)", actual, ok, identity)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	protectedRequest := httptest.NewRequest(http.MethodGet, "/admin/tokens/user-1", nil)
	protectedRequest.AddCookie(cookies[0])
	protectedResponse := httptest.NewRecorder()
	protected.ServeHTTP(protectedResponse, protectedRequest)
	if protectedResponse.Code != http.StatusNoContent {
		t.Fatalf("session protected status = %d, want %d", protectedResponse.Code, http.StatusNoContent)
	}
}

func TestHandlerRequireAdmin(t *testing.T) {
	t.Parallel()
	handler, identity := testHandler(t)
	cookie, err := handler.sessions.CreateCookie(identity)
	if err != nil {
		t.Fatalf("CreateCookie: %v", err)
	}
	protected := handler.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name          string
		method        string
		cookie        bool
		adminSecret   string
		requestedWith string
		wantStatus    int
	}{
		{name: "legacy admin secret", method: http.MethodPost, adminSecret: "admin-secret", wantStatus: http.StatusNoContent},
		{name: "session safe request", method: http.MethodGet, cookie: true, wantStatus: http.StatusNoContent},
		{name: "session mutation requires csrf header", method: http.MethodPost, cookie: true, wantStatus: http.StatusUnauthorized},
		{name: "session mutation with csrf header", method: http.MethodPost, cookie: true, requestedWith: csrfHeaderValue, wantStatus: http.StatusNoContent},
		{name: "invalid legacy admin secret", method: http.MethodPost, adminSecret: "wrong", wantStatus: http.StatusUnauthorized},
		{name: "no credentials", method: http.MethodGet, wantStatus: http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/admin/keys", nil)
			if test.cookie {
				request.AddCookie(cookie)
			}
			if test.adminSecret != "" {
				request.Header.Set("X-Admin-Secret", test.adminSecret)
			}
			if test.requestedWith != "" {
				request.Header.Set(csrfHeaderName, test.requestedWith)
			}
			response := httptest.NewRecorder()
			protected.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("RequireAdmin status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestHandlerLoginRejectsInvalidCredentialsAndLogoutClearsCookie(t *testing.T) {
	t.Parallel()
	handler, identity := testHandler(t)
	form := url.Values{"email": {"admin@example.com"}, "password": {"wrong password"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBufferString(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.Login(response, request)
	if response.Code != http.StatusUnauthorized || !bytes.Contains(response.Body.Bytes(), []byte("Email or password is incorrect.")) {
		t.Fatalf("invalid login = status %d body %q", response.Code, response.Body.String())
	}

	cookie, err := handler.sessions.CreateCookie(identity)
	if err != nil {
		t.Fatalf("CreateCookie: %v", err)
	}
	logout := handler.RequireAdmin(http.HandlerFunc(handler.Logout))
	logoutRequest := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	logoutRequest.AddCookie(cookie)
	logoutRequest.Header.Set(csrfHeaderName, csrfHeaderValue)
	logoutResponse := httptest.NewRecorder()
	logout.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusSeeOther {
		t.Fatalf("Logout status = %d, want %d", logoutResponse.Code, http.StatusSeeOther)
	}
	cookies := logoutResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("Logout cookies = %#v", cookies)
	}
}

func TestHandlerLoginRateLimit(t *testing.T) {
	t.Parallel()
	handler, _ := testHandler(t)
	form := url.Values{"email": {"admin@example.com"}, "password": {"wrong password"}}

	for attempt := 0; attempt < 5; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBufferString(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.RemoteAddr = "192.0.2.1:1234"
		response := httptest.NewRecorder()
		handler.Login(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, response.Code, http.StatusUnauthorized)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/admin/login", bytes.NewBufferString(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.Login(response, request)
	if response.Code != http.StatusTooManyRequests || !bytes.Contains(response.Body.Bytes(), []byte("Too many sign-in attempts")) {
		t.Fatalf("rate-limited login = status %d body %q", response.Code, response.Body.String())
	}
}
