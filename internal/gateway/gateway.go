/*
Copyright 2026 Clawdlinux.

Licensed under the Business Source License 1.1.
See LICENSE in the repository root.
*/

// Package gateway implements the HTTP API that agents call to act on SaaS
// services on behalf of users.
//
// Core endpoints:
//   - POST /v1/act          — execute an action on a service
//   - GET  /v1/services     — list available services
//   - GET  /v1/services/:id — describe a service and its actions
//   - GET  /healthz         — health check
//
// The gateway authenticates agents via API key, looks up the service and
// action in the registry, fetches the user's token from the vault, builds
// the upstream request, injects auth, and proxies the response.
package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

// Config holds the gateway dependencies.
type Config struct {
	Registry   *registry.Registry
	Vault      vault.Store
	HTTPClient *http.Client
	Logger     *slog.Logger
	AgentKeys  map[string]string // api_key -> agent_id (simple MVP auth)
}

// Server is the gateway HTTP server.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	logger *slog.Logger
}

// New creates a gateway server.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Server{cfg: cfg, logger: cfg.Logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/services", s.handleListServices)
	mux.HandleFunc("GET /v1/services/{name}", s.handleDescribeService)
	mux.HandleFunc("POST /v1/act", s.handleAct)
	s.mux = mux
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ActRequest is the body of POST /v1/act.
type ActRequest struct {
	Service    string                 `json:"service"`
	Action     string                 `json:"action"`
	OnBehalfOf string                 `json:"on_behalf_of"`
	Params     map[string]interface{} `json:"params,omitempty"`
}

// ActResponse wraps the upstream response.
type ActResponse struct {
	Status     int             `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
	BodyText   string          `json:"body_text,omitempty"`
	LatencyMS  int64           `json:"latency_ms"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"services": fmt.Sprintf("%d", s.cfg.Registry.Count()),
	})
}

func (s *Server) handleListServices(w http.ResponseWriter, _ *http.Request) {
	names := s.cfg.Registry.List()
	writeJSON(w, http.StatusOK, map[string]interface{}{"services": names})
}

func (s *Server) handleDescribeService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	svc, err := s.cfg.Registry.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error(), Code: "service_not_found"})
		return
	}

	actions := make(map[string]interface{})
	for aName, a := range svc.Actions {
		actions[aName] = map[string]interface{}{
			"method": a.Method,
			"path":   a.Path,
			"params": a.Params,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":      svc.Name,
		"base_url":  svc.BaseURL,
		"auth_type": svc.Auth.Type,
		"actions":   actions,
	})
}

func (s *Server) handleAct(w http.ResponseWriter, r *http.Request) {
	// Authenticate agent.
	agentID, ok := s.authenticateAgent(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{
			Error: "invalid or missing API key",
			Code:  "unauthorized",
		})
		return
	}

	// Parse request.
	var req ActRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
			Code:  "bad_request",
		})
		return
	}

	if req.Service == "" || req.Action == "" || req.OnBehalfOf == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "service, action, and on_behalf_of are required",
			Code:  "bad_request",
		})
		return
	}

	// Look up service + action.
	svc, action, err := s.cfg.Registry.GetAction(req.Service, req.Action)
	if err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error:   err.Error(),
			Code:    "not_found",
		})
		return
	}

	// Fetch user's token.
	tok, err := s.cfg.Vault.Get(req.OnBehalfOf, req.Service)
	if err != nil {
		writeJSON(w, http.StatusForbidden, ErrorResponse{
			Error:   fmt.Sprintf("no token for user %s on service %s — user must connect their account first", req.OnBehalfOf, req.Service),
			Code:    "token_missing",
		})
		return
	}

	if tok.IsExpired() {
		// TODO: auto-refresh using refresh_token
		writeJSON(w, http.StatusForbidden, ErrorResponse{
			Error: "token expired — user must re-authenticate",
			Code:  "token_expired",
		})
		return
	}

	// Build upstream URL.
	upstreamURL := buildURL(svc.BaseURL, action.Path, req.Params)

	// Build upstream request.
	var bodyReader io.Reader
	if action.Method == "POST" || action.Method == "PUT" || action.Method == "PATCH" {
		bodyBytes, _ := json.Marshal(req.Params)
		bodyReader = strings.NewReader(string(bodyBytes))
	}

	upstream, err := http.NewRequestWithContext(r.Context(), action.Method, upstreamURL, bodyReader)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "failed to build upstream request",
			Code:  "internal",
		})
		return
	}

	// Inject auth — the core value proposition.
	injectAuth(upstream, svc.Auth, tok)

	if bodyReader != nil {
		upstream.Header.Set("Content-Type", "application/json")
	}
	upstream.Header.Set("Accept", "application/json")

	// Execute.
	start := time.Now()
	resp, err := s.cfg.HTTPClient.Do(upstream)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		writeJSON(w, http.StatusBadGateway, ErrorResponse{
			Error:   fmt.Sprintf("upstream request failed: %v", err),
			Code:    "upstream_error",
		})
		return
	}
	defer resp.Body.Close()

	// Read upstream response.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit

	s.logger.Info("act",
		"agent", agentID,
		"user", req.OnBehalfOf,
		"service", req.Service,
		"action", req.Action,
		"upstream_status", resp.StatusCode,
		"latency_ms", latency,
	)

	// Determine if body is JSON.
	var jsonBody json.RawMessage
	if json.Unmarshal(body, &jsonBody) == nil {
		writeJSON(w, resp.StatusCode, ActResponse{
			Status:    resp.StatusCode,
			Body:      jsonBody,
			LatencyMS: latency,
		})
	} else {
		writeJSON(w, resp.StatusCode, ActResponse{
			Status:    resp.StatusCode,
			BodyText:  string(body),
			LatencyMS: latency,
		})
	}
}

func (s *Server) authenticateAgent(r *http.Request) (string, bool) {
	key := r.Header.Get("Authorization")
	key = strings.TrimPrefix(key, "Bearer ")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	agentID, ok := s.cfg.AgentKeys[key]
	return agentID, ok
}

// injectAuth adds authentication headers to the upstream request.
// The agent NEVER sees these credentials — they come from the vault.
func injectAuth(r *http.Request, auth registry.AuthCfg, tok vault.Token) {
	switch auth.Type {
	case "oauth2", "bearer":
		r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	case "api_key":
		header := auth.HeaderName
		if header == "" {
			header = "Authorization"
		}
		r.Header.Set(header, tok.AccessToken)
	}
}

// buildURL constructs the full upstream URL with path parameters substituted.
func buildURL(baseURL, pathTemplate string, params map[string]interface{}) string {
	path := pathTemplate
	for k, v := range params {
		placeholder := "{" + k + "}"
		if strings.Contains(path, placeholder) {
			path = strings.ReplaceAll(path, placeholder, fmt.Sprint(v))
		}
	}
	return strings.TrimRight(baseURL, "/") + path
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
