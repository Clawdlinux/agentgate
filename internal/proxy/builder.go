package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

// UpstreamRequest is a fully-built request ready to execute against upstream.
type UpstreamRequest struct {
	Method  string
	URL     string
	Headers http.Header
	Body    io.Reader
}

// BuildRequest constructs the upstream HTTP request from service config, action,
// token, and agent-supplied params.
//
// Path params (e.g. {owner}) are substituted from params.
// For GET/DELETE: remaining params become query string.
// For POST/PUT/PATCH: remaining params become JSON body.
func BuildRequest(svc registry.Service, action registry.Action, tok vault.Token, params map[string]interface{}) (*UpstreamRequest, error) {
	// Substitute path params.
	path := action.Path
	usedParams := make(map[string]bool)
	for k, v := range params {
		placeholder := "{" + k + "}"
		if strings.Contains(path, placeholder) {
			path = strings.ReplaceAll(path, placeholder, fmt.Sprint(v))
			usedParams[k] = true
		}
	}

	// Check for unsubstituted path params.
	if strings.Contains(path, "{") {
		return nil, fmt.Errorf("proxy: unresolved path params in %s", path)
	}

	fullURL := strings.TrimRight(svc.BaseURL, "/") + path
	headers := make(http.Header)

	// Inject auth.
	injectAuth(headers, svc.Auth, tok)
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "AgentGate/0.1.0")

	var body io.Reader

	switch action.Method {
	case "GET", "DELETE", "HEAD":
		// Remaining params → query string.
		u, err := url.Parse(fullURL)
		if err != nil {
			return nil, fmt.Errorf("proxy: parse url: %w", err)
		}
		q := u.Query()
		for k, v := range params {
			if usedParams[k] {
				continue
			}
			q.Set(k, fmt.Sprint(v))
		}
		u.RawQuery = q.Encode()
		fullURL = u.String()

	case "POST", "PUT", "PATCH":
		// Remaining params → JSON body.
		remaining := make(map[string]interface{})
		for k, v := range params {
			if usedParams[k] {
				continue
			}
			remaining[k] = v
		}
		if len(remaining) > 0 {
			bodyBytes, err := json.Marshal(remaining)
			if err != nil {
				return nil, fmt.Errorf("proxy: marshal body: %w", err)
			}
			body = bytes.NewReader(bodyBytes)
			headers.Set("Content-Type", "application/json")
		}
	}

	return &UpstreamRequest{
		Method:  action.Method,
		URL:     fullURL,
		Headers: headers,
		Body:    body,
	}, nil
}

// injectAuth adds auth headers based on service config.
func injectAuth(h http.Header, auth registry.AuthCfg, tok vault.Token) {
	switch auth.Type {
	case "oauth2", "bearer":
		h.Set("Authorization", "Bearer "+tok.AccessToken)
	case "api_key":
		header := auth.HeaderName
		if header == "" {
			header = "Authorization"
		}
		h.Set(header, tok.AccessToken)
	}
}
