package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/oauth"
	"github.com/Clawdlinux/agentgate/internal/org"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
	"golang.org/x/time/rate"
)

// Handler provides admin API endpoints for key management and user linking.
type Handler struct {
	keyStore     *auth.KeyStore
	oauthHandler *oauth.CallbackHandler
	vault        vault.Store
	registry     *registry.Registry
	orgStore     *org.Store
	sessions     *SessionManager
	adminSecret  string
	logger       *slog.Logger
	loginLimits  sync.Map
}

// NewHandler creates the admin handler.
func NewHandler(ks *auth.KeyStore, oh *oauth.CallbackHandler, v vault.Store, reg *registry.Registry, orgStore *org.Store, sessions *SessionManager, adminSecret string, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		keyStore:     ks,
		oauthHandler: oh,
		vault:        v,
		registry:     reg,
		orgStore:     orgStore,
		sessions:     sessions,
		adminSecret:  adminSecret,
		logger:       logger,
	}
}

type adminContextKey string

const adminIdentityContextKey adminContextKey = "admin_identity"

const csrfHeaderName = "X-Requested-With"

const csrfHeaderValue = "AgentGate"

func AdminFromContext(ctx context.Context) (SessionIdentity, bool) {
	identity, ok := ctx.Value(adminIdentityContextKey).(SessionIdentity)
	return identity, ok
}

// RequireAdmin accepts the legacy X-Admin-Secret header or an authenticated session cookie.
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := r.Header.Get("X-Admin-Secret")
		if secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(h.adminSecret)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		identity, err := h.sessions.Authenticate(r)
		if err != nil || (isMutatingRequest(r) && r.Header.Get(csrfHeaderName) != csrfHeaderValue) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "invalid admin secret",
				"code":  "unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminIdentityContextKey, identity)))
	})
}

func isMutatingRequest(r *http.Request) bool {
	return r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
}

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	renderLoginPage(w, http.StatusOK, false, false)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.allowLogin(r) {
		renderLoginPage(w, http.StatusTooManyRequests, false, true)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		renderLoginPage(w, http.StatusBadRequest, false, false)
		return
	}
	admin, err := h.orgStore.Authenticate(r.Context(), r.FormValue("email"), r.FormValue("password"))
	if err != nil {
		if err == org.ErrInvalidCredentials {
			renderLoginPage(w, http.StatusUnauthorized, true, false)
			return
		}
		h.logger.Error("admin login failed", "error", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	cookie, err := h.sessions.CreateCookie(SessionIdentity{AdminID: admin.ID, OrgID: admin.OrgID})
	if err != nil {
		h.logger.Error("create admin session", "error", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, h.sessions.ClearCookie())
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *Handler) allowLogin(r *http.Request) bool {
	remoteAddress := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteAddress = host
	}
	limiter, _ := h.loginLimits.LoadOrStore(remoteAddress, rate.NewLimiter(rate.Every(12*time.Second), 5))
	return limiter.(*rate.Limiter).Allow()
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>AgentGate Admin Login</title><style>body{margin:0;background:#101318;color:#e9edf1;font:16px system-ui,sans-serif;display:grid;min-height:100vh;place-items:center}main{width:min(26rem,calc(100% - 2rem));border:1px solid #3a424c;padding:2rem;background:#151a20}h1{margin-top:0}label,input,button{display:block;width:100%;box-sizing:border-box}label{margin-top:1rem}input{margin-top:.4rem;padding:.7rem;background:#0d1117;border:1px solid #59636f;color:#e9edf1}button{margin-top:1.5rem;padding:.75rem;border:0;background:#60d6c5;color:#07110f;font-weight:700}.error{color:#ffb4ab}</style></head>
<body><main><h1>AgentGate Admin</h1>{{if .InvalidCredentials}}<p class="error">Email or password is incorrect.</p>{{end}}{{if .RateLimited}}<p class="error">Too many sign-in attempts. Try again later.</p>{{end}}<form method="post" action="/admin/login"><label>Email<input type="email" name="email" autocomplete="username" required></label><label>Password<input type="password" name="password" autocomplete="current-password" required></label><button type="submit">Sign in</button></form></main></body></html>`))

func renderLoginPage(w http.ResponseWriter, status int, invalidCredentials, rateLimited bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, struct {
		InvalidCredentials bool
		RateLimited        bool
	}{InvalidCredentials: invalidCredentials, RateLimited: rateLimited}); err != nil {
		http.Error(w, "render login page", http.StatusInternalServerError)
	}
}

// CreateKey handles POST /admin/keys.
func (h *Handler) CreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string   `json:"name"`
		AllowedServices []string `json:"allowed_services"`
		AllowedUsers    []string `json:"allowed_users"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if len(req.AllowedServices) == 0 {
		req.AllowedServices = []string{"*"}
	}
	if len(req.AllowedUsers) == 0 {
		req.AllowedUsers = []string{"*"}
	}

	key, plaintext, err := h.keyStore.Create(r.Context(), req.Name, req.AllowedServices, req.AllowedUsers)
	if err != nil {
		h.logger.Error("create key failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create key"})
		return
	}

	h.logger.Info("admin: key created", "id", key.ID, "name", key.Name)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":               key.ID,
		"name":             key.Name,
		"key":              plaintext,
		"allowed_services": key.AllowedServices,
		"allowed_users":    key.AllowedUsers,
		"note":             "Save this key — it will not be shown again.",
	})
}

// RevokeKey handles DELETE /admin/keys/{id}.
func (h *Handler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key id required"})
		return
	}

	if err := h.keyStore.Revoke(r.Context(), id); err != nil {
		if err == auth.ErrKeyNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "key not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "revoke failed"})
		}
		return
	}

	h.logger.Info("admin: key revoked", "id", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// LinkAccount handles POST /admin/link.
func (h *Handler) LinkAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string `json:"user_id"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.UserID == "" || req.Service == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id and service are required"})
		return
	}

	authorizeURL, err := h.oauthHandler.GetAuthorizeURL(req.UserID, req.Service)
	if err != nil {
		h.logger.Error("link account failed", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"authorize_url": authorizeURL})
}

// ConnectBearerToken stores a bearer token for a configured bearer service.
func (h *Handler) ConnectBearerToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID      string `json:"user_id"`
		Service     string `json:"service"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Service = strings.TrimSpace(req.Service)
	if req.UserID == "" || req.Service == "" || strings.TrimSpace(req.AccessToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id, service, and access_token are required"})
		return
	}
	svc, err := h.registry.Get(req.Service)
	if err != nil || svc.Auth.Type != "bearer" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service does not accept bearer token connections"})
		return
	}
	if err := h.vault.Put(req.UserID, req.Service, vault.Token{AccessToken: req.AccessToken, TokenType: "Bearer"}); err != nil {
		h.logger.Error("store bearer token failed", "service", req.Service, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store bearer token"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListTokens handles GET /admin/tokens/{user_id}.
// Returns linked services (no token values!).
func (h *Handler) ListTokens(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}

	services, err := h.vault.ListServices(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list services"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":  userID,
		"services": services,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
