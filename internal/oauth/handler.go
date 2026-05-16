package oauth

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Clawdlinux/agentgate/internal/vault"
)

// Provider holds OAuth configuration for one service.
type Provider struct {
	Name         string
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	Scopes       []string
}

// CallbackHandler handles GET /auth/callback/{service}.
// It exchanges the authorization code for tokens and stores them in the vault.
type CallbackHandler struct {
	providers    map[string]*Provider
	vault        vault.Store
	encKey       []byte
	callbackBase string
	logger       *slog.Logger
}

// NewCallbackHandler creates a new OAuth callback handler.
func NewCallbackHandler(providers map[string]*Provider, v vault.Store, encKey []byte, callbackBase string, logger *slog.Logger) *CallbackHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &CallbackHandler{
		providers:    providers,
		vault:        v,
		encKey:       encKey,
		callbackBase: callbackBase,
		logger:       logger,
	}
}

// GetAuthorizeURL generates the OAuth authorization URL for a user + service.
func (h *CallbackHandler) GetAuthorizeURL(userID, service string) (string, error) {
	provider, ok := h.providers[service]
	if !ok {
		return "", fmt.Errorf("oauth: unknown provider: %s", service)
	}

	state, err := EncryptState(userID, service, h.encKey)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(provider.AuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("oauth: parse authorize url: %w", err)
	}

	q := u.Query()
	q.Set("client_id", provider.ClientID)
	q.Set("redirect_uri", h.callbackBase+"/auth/callback/"+service)
	q.Set("state", state)
	q.Set("response_type", "code")
	if len(provider.Scopes) > 0 {
		q.Set("scope", strings.Join(provider.Scopes, " "))
	}
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// ServeHTTP handles the OAuth callback.
func (h *CallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	service := r.PathValue("service")
	if service == "" {
		http.Error(w, "missing service", http.StatusBadRequest)
		return
	}

	provider, ok := h.providers[service]
	if !ok {
		http.Error(w, "unknown service", http.StatusNotFound)
		return
	}

	// Check for error from OAuth provider.
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		h.logger.Error("oauth callback error", "service", service, "error", errMsg)
		http.Error(w, "OAuth error: "+errMsg, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	// Decrypt and validate state.
	userID, stateSvc, err := DecryptState(state, h.encKey)
	if err != nil {
		h.logger.Error("oauth state invalid", "error", err)
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}
	if stateSvc != service {
		http.Error(w, "state service mismatch", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens.
	tok, err := h.exchangeCode(provider, code, service)
	if err != nil {
		h.logger.Error("oauth code exchange failed", "service", service, "error", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	// Store in vault.
	if err := h.vault.Put(userID, service, *tok); err != nil {
		h.logger.Error("vault store failed", "error", err)
		http.Error(w, "failed to store token", http.StatusInternalServerError)
		return
	}

	h.logger.Info("oauth linked", "user", userID, "service", service)

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><body>
<h2>Account linked!</h2>
<p>Your %s account has been connected. You can close this tab.</p>
</body></html>`, service)
}

func (h *CallbackHandler) exchangeCode(provider *Provider, code, service string) (*vault.Token, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {provider.ClientID},
		"client_secret": {provider.ClientSecret},
		"redirect_uri":  {h.callbackBase + "/auth/callback/" + service},
	}

	resp, err := http.PostForm(provider.TokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("oauth: exchange: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oauth: exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("oauth: parse token response: %w", err)
	}

	tok := &vault.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
	}
	if tokenResp.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	if tokenResp.Scope != "" {
		tok.Scopes = strings.Split(tokenResp.Scope, " ")
	}

	return tok, nil
}
