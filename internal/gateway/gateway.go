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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Clawdlinux/agentgate/internal/auth"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/registry"
	upstreamclient "github.com/Clawdlinux/agentgate/internal/upstream"
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

// DelegationVerifier checks a presented Biscuit delegation token against
// the specific request it accompanies and returns the ordered attenuation
// commitments to store in the receipt (DELG-01, DELG-02, DELG-03). Backed
// by *delegation.Verifier in production.
type DelegationVerifier interface {
	Verify(token []byte, humanPrincipal, agentKeyID, service, action string, now time.Time) ([]string, error)
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
	HTTPClient *http.Client // optional embedded client; upstream deadlines use request contexts
	Logger     *slog.Logger
	Authorizer AgentAuthorizer              // required: verifies API keys and scopes
	Receipts   ReceiptRecorder              // required: commits one receipt per attempt
	Limiter    RequestLimiter               // optional: nil disables rate limiting
	Delegation DelegationVerifier           // optional: nil rejects any request presenting a delegation token
	Refreshers map[string]vault.RefreshFunc // optional: refreshes expiring OAuth tokens by service
}

// Server is the gateway HTTP server.
type Server struct {
	cfg       Config
	mux       *http.ServeMux
	logger    *slog.Logger
	upstreams *upstreamclient.Pool
}

// New creates a gateway server. Authorizer and Receipts must not be nil —
// every authenticated action attempt must reach a receipt (LEDG-04).
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Authorizer == nil {
		panic("gateway: Config.Authorizer must not be nil")
	}
	if cfg.Receipts == nil {
		panic("gateway: Config.Receipts must not be nil")
	}
	s := &Server{cfg: cfg, logger: cfg.Logger, upstreams: upstreamclient.New(upstreamclient.Options{Client: cfg.HTTPClient})}
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
// Params retains raw JSON for unchanged upstream request bodies.
// Its decoded value is reused for the JCS receipt hash and URL construction.
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
			"stream": a.Stream,
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
	agentKey        *auth.AgentKey
	req             ActRequest
	paramsHash      [32]byte
	params          map[string]any
	acceptSSE       bool
	delegationToken []byte   // raw, not yet verified; set by prepareAttempt, verified by executeAttempt
	delegationChain []string // set by executeAttempt once delegationToken verifies; empty for a direct grant (DELG-04)
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
	stream         *streamResponse
}

func (s *Server) handleAct(w http.ResponseWriter, r *http.Request) {
	att, immediate := s.prepareAttempt(r)
	if immediate != nil {
		immediate.write(w)
		return
	}

	start := time.Now()
	if _, ok := w.(http.Flusher); !ok {
		att.acceptSSE = false
	}
	out := s.executeAttempt(r.Context(), att)
	if out.stream != nil {
		defer out.stream.close()
		stopCancel := context.AfterFunc(r.Context(), out.stream.close)
		defer stopCancel()
	}
	latencyMS := time.Since(start).Milliseconds()

	// A client cancellation must not abort the still-pending receipt
	// commit — use a bounded context detached from the request's own
	// cancellation (LEDG-07).
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), receiptAppendTimeout)
	defer cancel()

	draft := receipt.Draft{
		HumanPrincipal:  att.req.OnBehalfOf,
		AgentKeyID:      att.agentKey.ID,
		DelegationChain: att.delegationChain,
		Service:         att.req.Service,
		Action:          att.req.Action,
		ParamsSHA256:    att.paramsHash,
		PolicyDecision:  out.policyDecision,
		StatusCode:      out.status,
		LatencyMS:       latencyMS,
		Error:           out.errorCode,
	}

	if _, err := s.cfg.Receipts.Append(receiptCtx, draft); err != nil {
		s.logger.Error("receipt append failed",
			"agent", att.agentKey.ID,
			"service", att.req.Service,
			"action", att.req.Action,
			"error", err,
		)
		// Never return the would-be outcome without an acknowledged receipt commit (LEDG-07).
		// A timed-out append may still commit.
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
	if out.stream != nil {
		s.writeStream(w, r, att, out.stream)
		return
	}
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

	params, paramsHash, err := receipt.ParseAndDigestParams(req.Params)
	if err != nil {
		return nil, &immediateResponse{status: http.StatusBadRequest, body: ErrorResponse{
			Error: "invalid params",
			Code:  "bad_request",
		}}
	}

	return &attempt{
		agentKey: agentKey, req: req, params: params, paramsHash: paramsHash,
		acceptSSE: acceptsEventStream(r.Header), delegationToken: extractDelegationToken(r),
	}, nil
}

// executeAttempt runs every check and side effect after authentication and
// shape validation: delegation, scope, rate limit, registry lookup, vault
// fetch, and upstream dispatch. It never writes to an http.ResponseWriter —
// every exit is captured in outcome so handleAct can commit a receipt
// before any response is written.
func (s *Server) executeAttempt(ctx context.Context, att *attempt) *outcome {
	if len(att.delegationToken) > 0 {
		// Verified before registry, vault, or upstream access (DELG-01); a
		// gateway with no configured verifier fails closed rather than
		// silently accepting an unverified claim of delegated authority.
		if s.cfg.Delegation == nil {
			return &outcome{
				status:         http.StatusForbidden,
				body:           ErrorResponse{Error: "delegation is not configured on this gateway", Code: "delegation_denied"},
				policyDecision: "deny",
				errorCode:      "delegation_denied",
			}
		}
		chain, err := s.cfg.Delegation.Verify(att.delegationToken, att.req.OnBehalfOf, att.agentKey.ID, att.req.Service, att.req.Action, time.Now())
		if err != nil {
			return &outcome{
				status:         http.StatusForbidden,
				body:           ErrorResponse{Error: "delegation denied", Code: "delegation_denied"},
				policyDecision: "deny",
				errorCode:      "delegation_denied",
			}
		}
		att.delegationChain = chain
	}

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
	if refresher := s.cfg.Refreshers[att.req.Service]; refresher != nil {
		tok, err = vault.GetOrRefresh(s.cfg.Vault, att.req.OnBehalfOf, att.req.Service, refresher)
		if err != nil {
			return &outcome{
				status:         http.StatusForbidden,
				body:           ErrorResponse{Error: "token expired — user must re-authenticate", Code: "token_expired"},
				policyDecision: "allow",
				errorCode:      "token_expired",
			}
		}
	}
	if tok.IsExpired() {
		return &outcome{
			status:         http.StatusForbidden,
			body:           ErrorResponse{Error: "token expired — user must re-authenticate", Code: "token_expired"},
			policyDecision: "allow",
			errorCode:      "token_expired",
		}
	}

	upstreamURL, err := buildURL(svc.BaseURL, action.Path, att.params)
	if err != nil {
		return &outcome{
			status:         http.StatusBadRequest,
			body:           ErrorResponse{Error: "invalid params", Code: "bad_request"},
			policyDecision: "deny",
			errorCode:      "bad_request",
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
	streaming := action.Stream && att.acceptSSE
	dispatch := s.upstreams.Do
	if streaming {
		upstream.Header.Set("Accept", "text/event-stream")
		dispatch = s.upstreams.DoStream
	}

	resp, err := dispatch(svc.Name, svc.Upstream, upstream, action.Idempotent)
	if err != nil {
		status, code, message := http.StatusBadGateway, "upstream_error", "upstream request failed"
		if errors.Is(err, upstreamclient.ErrUnavailable) {
			status, code, message = http.StatusServiceUnavailable, "upstream_unavailable", "upstream unavailable"
		}
		s.logUpstreamError(svc.Name, err)
		return &outcome{
			status:         status,
			body:           ErrorResponse{Error: message, Code: code},
			policyDecision: "allow",
			errorCode:      code,
		}
	}
	if streaming && upstreamclient.IsEventStream(resp) {
		return &outcome{
			status: http.StatusOK, policyDecision: "allow",
			stream: &streamResponse{
				body:  resp.Body,
				limit: svc.Upstream.StreamLimit(), idle: svc.Upstream.StreamIdle(),
			},
		}
	}
	defer resp.Body.Close()

	limit := svc.Upstream.ResponseLimit()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(body)) > limit {
		return &outcome{
			status: http.StatusBadGateway, policyDecision: "allow",
			body: ErrorResponse{
				Error: "upstream response exceeds configured limit",
				Code:  "upstream_response_too_large",
			},
			errorCode: "upstream_response_too_large",
		}
	}
	if err != nil {
		s.logUpstreamError(svc.Name, err)
		return &outcome{
			status: http.StatusBadGateway, policyDecision: "allow",
			body:      ErrorResponse{Error: "upstream request failed", Code: "upstream_error"},
			errorCode: "upstream_error",
		}
	}

	if json.Valid(body) {
		return &outcome{
			status:         resp.StatusCode,
			body:           ActResponse{Status: resp.StatusCode, Body: json.RawMessage(body)},
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

// extractDelegationToken returns the raw Biscuit token bytes from the
// X-Agentgate-Delegation header, base64-decoded (standard encoding). An
// absent or empty header means no delegation is being claimed.
func extractDelegationToken(r *http.Request) []byte {
	encoded := strings.TrimSpace(r.Header.Get("X-Agentgate-Delegation"))
	if encoded == "" {
		return nil
	}
	token, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return []byte{0} // non-empty, deliberately invalid: fails verification rather than being silently ignored
	}
	return token
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

var errInvalidPathParams = errors.New("invalid path params")

// buildURL scans the template once. Inserted values are escaped single path segments.
func buildURL(baseURL, pathTemplate string, params map[string]any) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("invalid base URL")
	}

	var path strings.Builder
	for len(pathTemplate) > 0 {
		start := strings.IndexByte(pathTemplate, '{')
		if start < 0 {
			path.WriteString(pathTemplate)
			break
		}
		path.WriteString(pathTemplate[:start])
		pathTemplate = pathTemplate[start+1:]
		end := strings.IndexByte(pathTemplate, '}')
		if end <= 0 {
			return "", errInvalidPathParams
		}
		value, ok := params[pathTemplate[:end]]
		if !ok {
			return "", errInvalidPathParams
		}
		escaped, err := escapePathParam(value)
		if err != nil {
			return "", err
		}
		path.WriteString(escaped)
		pathTemplate = pathTemplate[end+1:]
	}
	fullURL := strings.TrimRight(baseURL, "/") + path.String()
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Scheme != base.Scheme || parsed.Host != base.Host {
		return "", errors.New("invalid upstream URL origin")
	}
	return fullURL, nil
}

func escapePathParam(value any) (string, error) {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		text = fmt.Sprint(v)
	case json.Number:
		text = v.String()
		if !json.Valid([]byte(text)) {
			return "", errInvalidPathParams
		}
		// Check integrality without rounding large integers or near-integer fractions through float64.
		number, ok := new(big.Rat).SetString(text)
		if !ok || !number.IsInt() {
			return "", errInvalidPathParams
		}
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v {
			return "", errInvalidPathParams
		}
		text = strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return escapePathParam(float64(v))
	default:
		return "", errInvalidPathParams
	}
	if text == "" || text == "." || text == ".." {
		return "", errInvalidPathParams
	}
	return url.PathEscape(text), nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	if response, ok := v.(ActResponse); ok && len(response.Body) > 0 {
		writeActResponse(w, status, response)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
