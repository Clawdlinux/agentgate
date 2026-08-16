/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
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
// The gateway authenticates agents via API key, checks their scopes, looks
// up the service and action in the registry, fetches the user's token from
// the vault, builds the upstream request, injects auth, proxies the
// response, and commits one signed receipt for every authenticated,
// schema-valid attempt before writing the HTTP response (LEDG-04).
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

// maxRequestBodyBytes bounds the /v1/act request body.
const maxRequestBodyBytes = 1 << 20 // 1 MB

// receiptAppendTimeout bounds the receipt commit so a canceled client
// request cannot abort a receipt that must still be written (LEDG-07).
const receiptAppendTimeout = 5 * time.Second

// AgentAuthorizer validates a plaintext API key and returns the verified
// agent key, including its scopes. Backed by *auth.KeyStore in production.
type AgentAuthorizer interface {
	Validate(ctx context.Context, plaintext string) (*auth.AgentKey, error)
}

// RequestLimiter reports whether a request for (agentKeyID, service) is
// currently allowed. Backed by *ratelimit.Limiter in production. A nil
// Limiter in Config disables rate limiting.
type RequestLimiter interface {
	Allow(agentKeyID, service string) bool
}

// ReceiptRecorder commits one signed receipt for an action attempt.
// Backed by *receipt.Ledger in production.
type ReceiptRecorder interface {
	Append(ctx context.Context, draft receipt.Draft) (receipt.Receipt, error)
}

// Config holds the gateway dependencies.
type Config struct {
	Registry   *registry.Registry
	Vault      vault.Store
	HTTPClient *http.Client
	Logger     *slog.Logger
	Authorizer AgentAuthorizer // required: verifies API keys and scopes
	Receipts   ReceiptRecorder // required: commits one receipt per attempt
	Limiter    RequestLimiter  // optional: nil disables rate limiting
}

// Server is the gateway HTTP server.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	logger *slog.Logger
}

// New creates a gateway server. Authorizer and Receipts must not be nil —
// every authenticated action attempt must reach a receipt (LEDG-04).
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Authorizer == nil {
		panic("gateway: Config.Authorizer must not be nil")
	}
	if cfg.Receipts == nil {
		panic("gateway: Config.Receipts must not be nil")
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

// ActRequest is the body of POST /v1/act. Params stays as raw JSON so its
// bytes can be hashed for the receipt and forwarded upstream unmodified,
// without a lossy decode/re-encode round trip.
type ActRequest struct {
	Service    string          `json:"service"`
	Action     string          `json:"action"`
	OnBehalfOf string          `json:"on_behalf_of"`
	Params     json.RawMessage `json:"params,omitempty"`
}

// ActResponse wraps the upstream response.
type ActResponse struct {
	Status    int               `json:"status"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      json.RawMessage   `json:"body,omitempty"`
	BodyText  string            `json:"body_text,omitempty"`
	LatencyMS int64             `json:"latency_ms"`
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

// attempt is everything needed to authorize, execute, and receipt one
// /v1/act call, once the request has a trustworthy agent identity and a
// schema-valid body.
type attempt struct {
	agentKey   *auth.AgentKey
	req        ActRequest
	paramsHash [32]byte
}

// immediateResponse is written with no receipt: the request never reached
// a trustworthy agent or action identity (Phase 2's receipted coverage
// boundary — unknown/revoked API keys and malformed request bodies).
type immediateResponse struct {
	status int
	body   any
}

func (ir *immediateResponse) write(w http.ResponseWriter) {
	writeJSON(w, ir.status, ir.body)
}

// outcome is the result of executeAttempt: every branch after
// authentication and shape-validation, ready to be turned into both an
// HTTP response and a receipt draft.
type outcome struct {
	status         int
	body           any
	policyDecision string // "allow", "deny", or "rate_limited"
	errorCode      string // stable code; empty means no error
}

func (s *Server) handleAct(w http.ResponseWriter, r *http.Request) {
	att, immediate := s.prepareAttempt(r)
	if immediate != nil {
		immediate.write(w)
		return
	}

	start := time.Now()
	out := s.executeAttempt(r.Context(), att)
	latencyMS := time.Since(start).Milliseconds()

	// A client cancellation must not abort the still-pending receipt
	// commit — use a bounded context detached from the request's own
	// cancellation (LEDG-07).
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), receiptAppendTimeout)
	defer cancel()

	draft := receipt.Draft{
		HumanPrincipal: att.req.OnBehalfOf,
		AgentKeyID:     att.agentKey.ID,
		Service:        att.req.Service,
		Action:         att.req.Action,
		ParamsSHA256:   att.paramsHash,
		PolicyDecision: out.policyDecision,
		StatusCode:     out.status,
		LatencyMS:      latencyMS,
		Error:          out.errorCode,
	}

	if _, err := s.cfg.Receipts.Append(receiptCtx, draft); err != nil {
		s.logger.Error("receipt append failed",
			"agent", att.agentKey.ID,
			"service", att.req.Service,
			"action", att.req.Action,
			"error", err,
		)
		// Never return the would-be outcome once its receipt failed to
		// commit (LEDG-07): the caller must not see a successful action
		// response that has no corresponding receipt.
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{
			Error: "receipt commit failed; action outcome not returned",
			Code:  "receipt_failed",
		})
		return
	}

	s.logger.Info("act",
		"agent", att.agentKey.ID,
		"user", att.req.OnBehalfOf,
		"service", att.req.Service,
		"action", att.req.Action,
		"policy_decision", out.policyDecision,
		"status", out.status,
		"latency_ms", latencyMS,
	)
	if actResp, ok := out.body.(ActResponse); ok {
		actResp.LatencyMS = latencyMS
		out.body = actResp
	}
	writeJSON(w, out.status, out.body)
}

// prepareAttempt authenticates the request and validates its shape. Its
// returns are mutually exclusive: a non-nil immediateResponse means the
// request never reaches executeAttempt or a receipt.
func (s *Server) prepareAttempt(r *http.Request) (*attempt, *immediateResponse) {
	key := extractAPIKey(r)
	if key == "" {
		return nil, &immediateResponse{status: http.StatusUnauthorized, body: ErrorResponse{
			Error: "invalid or missing API key",
			Code:  "unauthorized",
		}}
	}
	agentKey, err := s.cfg.Authorizer.Validate(r.Context(), key)
	if err != nil {
		return nil, &immediateResponse{status: http.StatusUnauthorized, body: ErrorResponse{
			Error: "invalid or missing API key",
			Code:  "unauthorized",
		}}
	}

	var req ActRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err := dec.Decode(&req); err != nil {
		return nil, &immediateResponse{status: http.StatusBadRequest, body: ErrorResponse{
			Error: "invalid request body",
			Code:  "bad_request",
		}}
	}
	if req.Service == "" || req.Action == "" || req.OnBehalfOf == "" {
		return nil, &immediateResponse{status: http.StatusBadRequest, body: ErrorResponse{
			Error: "service, action, and on_behalf_of are required",
			Code:  "bad_request",
		}}
	}

	paramsRaw := req.Params
	if len(paramsRaw) == 0 {
		paramsRaw = []byte("{}")
	}
	paramsHash, err := receipt.DigestParams(paramsRaw)
	if err != nil {
		return nil, &immediateResponse{status: http.StatusBadRequest, body: ErrorResponse{
			Error: "invalid params",
			Code:  "bad_request",
		}}
	}

	return &attempt{agentKey: agentKey, req: req, paramsHash: paramsHash}, nil
}

// executeAttempt runs every check and side effect after authentication and
// shape validation: scope, rate limit, registry lookup, vault fetch, and
// upstream dispatch. It never writes to an http.ResponseWriter — every
// exit is captured in outcome so handleAct can commit a receipt before any
// response is written.
func (s *Server) executeAttempt(ctx context.Context, att *attempt) *outcome {
	if !att.agentKey.CanAccessService(att.req.Service) || !att.agentKey.CanAccessUser(att.req.OnBehalfOf) {
		return &outcome{
			status:         http.StatusForbidden,
			body:           ErrorResponse{Error: "agent key is not scoped for this service or user", Code: "scope_denied"},
			policyDecision: "deny",
			errorCode:      "scope_denied",
		}
	}

	if s.cfg.Limiter != nil && !s.cfg.Limiter.Allow(att.agentKey.ID, att.req.Service) {
		return &outcome{
			status:         http.StatusTooManyRequests,
			body:           ErrorResponse{Error: "rate limit exceeded", Code: "rate_limited"},
			policyDecision: "rate_limited",
			errorCode:      "rate_limited",
		}
	}

	svc, action, err := s.cfg.Registry.GetAction(att.req.Service, att.req.Action)
	if err != nil {
		return &outcome{
			status:         http.StatusNotFound,
			body:           ErrorResponse{Error: err.Error(), Code: "not_found"},
			policyDecision: "deny",
			errorCode:      "not_found",
		}
	}

	tok, err := s.cfg.Vault.Get(att.req.OnBehalfOf, att.req.Service)
	if err != nil {
		return &outcome{
			status: http.StatusForbidden,
			body: ErrorResponse{
				Error: fmt.Sprintf("no token for user %s on service %s — user must connect their account first", att.req.OnBehalfOf, att.req.Service),
				Code:  "token_missing",
			},
			policyDecision: "allow",
			errorCode:      "token_missing",
		}
	}
	if tok.IsExpired() {
		// TODO: auto-refresh using refresh_token
		return &outcome{
			status:         http.StatusForbidden,
			body:           ErrorResponse{Error: "token expired — user must re-authenticate", Code: "token_expired"},
			policyDecision: "allow",
			errorCode:      "token_expired",
		}
	}

	upstreamURL, err := buildURL(svc.BaseURL, action.Path, att.req.Params)
	if err != nil {
		return &outcome{
			status:         http.StatusInternalServerError,
			body:           ErrorResponse{Error: "failed to build upstream request", Code: "internal"},
			policyDecision: "allow",
			errorCode:      "internal",
		}
	}

	var bodyReader io.Reader
	if action.Method == "POST" || action.Method == "PUT" || action.Method == "PATCH" {
		bodyReader = bytes.NewReader(att.req.Params)
	}

	upstream, err := http.NewRequestWithContext(ctx, action.Method, upstreamURL, bodyReader)
	if err != nil {
		return &outcome{
			status:         http.StatusInternalServerError,
			body:           ErrorResponse{Error: "failed to build upstream request", Code: "internal"},
			policyDecision: "allow",
			errorCode:      "internal",
		}
	}

	// Inject auth — the core value proposition. The agent never sees this
	// credential; it comes from the vault.
	injectAuth(upstream, svc.Auth, tok)
	if bodyReader != nil {
		upstream.Header.Set("Content-Type", "application/json")
	}
	upstream.Header.Set("Accept", "application/json")

	resp, err := s.cfg.HTTPClient.Do(upstream)
	if err != nil {
		return &outcome{
			status:         http.StatusBadGateway,
			body:           ErrorResponse{Error: fmt.Sprintf("upstream request failed: %v", err), Code: "upstream_error"},
			policyDecision: "allow",
			errorCode:      "upstream_error",
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit

	var jsonBody json.RawMessage
	if json.Unmarshal(body, &jsonBody) == nil {
		return &outcome{
			status:         resp.StatusCode,
			body:           ActResponse{Status: resp.StatusCode, Body: jsonBody},
			policyDecision: "allow",
		}
	}
	return &outcome{
		status:         resp.StatusCode,
		body:           ActResponse{Status: resp.StatusCode, BodyText: string(body)},
		policyDecision: "allow",
	}
}

func extractAPIKey(r *http.Request) string {
	key := r.Header.Get("Authorization")
	key = strings.TrimPrefix(key, "Bearer ")
	return strings.TrimSpace(key)
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

// buildURL constructs the full upstream URL with path parameters
// substituted from the raw params JSON. json.Number preserves the
// parameter's original numeric text instead of reformatting it through a
// float64 round trip.
func buildURL(baseURL, pathTemplate string, paramsRaw json.RawMessage) (string, error) {
	path := pathTemplate
	if len(paramsRaw) == 0 {
		return strings.TrimRight(baseURL, "/") + path, nil
	}

	var params map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(paramsRaw))
	dec.UseNumber()
	if err := dec.Decode(&params); err != nil {
		return "", err
	}
	for k, v := range params {
		placeholder := "{" + k + "}"
		if strings.Contains(path, placeholder) {
			path = strings.ReplaceAll(path, placeholder, fmt.Sprint(v))
		}
	}
	return strings.TrimRight(baseURL, "/") + path, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
