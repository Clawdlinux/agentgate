package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/oauth"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

// Handler provides admin API endpoints for key management and user linking.
type Handler struct {
	keyStore     *auth.KeyStore
	oauthHandler *oauth.CallbackHandler
	vault        vault.Store
	registry     *registry.Registry
	adminSecret  string
	logger       *slog.Logger
}

// NewHandler creates the admin handler.
func NewHandler(ks *auth.KeyStore, oh *oauth.CallbackHandler, v vault.Store, reg *registry.Registry, adminSecret string, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		keyStore:     ks,
		oauthHandler: oh,
		vault:        v,
		registry:     reg,
		adminSecret:  adminSecret,
		logger:       logger,
	}
}

// RequireAdmin is middleware that checks the X-Admin-Secret header.
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := r.Header.Get("X-Admin-Secret")
		if secret == "" || secret != h.adminSecret {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "invalid admin secret",
				"code":  "unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
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
