package proxy

import (
	"io"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/registry"
	"github.com/Clawdlinux/agentgate/internal/vault"
)

func TestBuildRequest_GET_QueryParams(t *testing.T) {
	t.Parallel()
	svc := registry.Service{
		BaseURL: "https://api.stripe.com/v1",
		Auth:    registry.AuthCfg{Type: "bearer"},
	}
	action := registry.Action{
		Method: "GET",
		Path:   "/invoices",
	}
	tok := vault.Token{AccessToken: "sk_test_123"}
	params := map[string]interface{}{"limit": 10, "status": "open"}

	req, err := BuildRequest(svc, action, tok, params)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" {
		t.Fatalf("method = %s", req.Method)
	}
	if !strings.Contains(req.URL, "limit=10") {
		t.Fatalf("URL missing limit param: %s", req.URL)
	}
	if !strings.Contains(req.URL, "status=open") {
		t.Fatalf("URL missing status param: %s", req.URL)
	}
	if req.Headers.Get("Authorization") != "Bearer sk_test_123" {
		t.Fatalf("auth = %s", req.Headers.Get("Authorization"))
	}
}

func TestBuildRequest_POST_JSONBody(t *testing.T) {
	t.Parallel()
	svc := registry.Service{
		BaseURL: "https://api.github.com",
		Auth:    registry.AuthCfg{Type: "oauth2"},
	}
	action := registry.Action{
		Method: "POST",
		Path:   "/repos/{owner}/{repo}/issues",
	}
	tok := vault.Token{AccessToken: "ghp_abc"}
	params := map[string]interface{}{
		"owner": "clawdlinux",
		"repo":  "agentgate",
		"title": "Test issue",
		"body":  "This is a test",
	}

	req, err := BuildRequest(svc, action, tok, params)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" {
		t.Fatalf("method = %s", req.Method)
	}
	if !strings.Contains(req.URL, "/repos/clawdlinux/agentgate/issues") {
		t.Fatalf("URL = %s", req.URL)
	}
	if req.Headers.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %s", req.Headers.Get("Content-Type"))
	}
	// Body should contain title and body but NOT owner/repo (used as path params).
	bodyBytes, _ := io.ReadAll(req.Body)
	bodyStr := string(bodyBytes)
	if !strings.Contains(bodyStr, "Test issue") {
		t.Fatalf("body missing title: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "clawdlinux") {
		t.Fatalf("body should not contain path params: %s", bodyStr)
	}
}

func TestBuildRequest_PathParams(t *testing.T) {
	t.Parallel()
	svc := registry.Service{
		BaseURL: "https://api.github.com",
		Auth:    registry.AuthCfg{Type: "oauth2"},
	}
	action := registry.Action{
		Method: "GET",
		Path:   "/repos/{owner}/{repo}/pulls",
	}
	tok := vault.Token{AccessToken: "ghp_abc"}
	params := map[string]interface{}{
		"owner": "clawdlinux",
		"repo":  "agentgate",
		"state": "open",
	}

	req, err := BuildRequest(svc, action, tok, params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.URL, "/repos/clawdlinux/agentgate/pulls") {
		t.Fatalf("URL path params not substituted: %s", req.URL)
	}
	if !strings.Contains(req.URL, "state=open") {
		t.Fatalf("URL missing query param: %s", req.URL)
	}
}

func TestBuildRequest_UnresolvedPathParam(t *testing.T) {
	t.Parallel()
	svc := registry.Service{
		BaseURL: "https://api.github.com",
		Auth:    registry.AuthCfg{Type: "oauth2"},
	}
	action := registry.Action{
		Method: "GET",
		Path:   "/repos/{owner}/{repo}/issues",
	}
	tok := vault.Token{AccessToken: "ghp_abc"}
	// Missing "repo" param.
	params := map[string]interface{}{"owner": "clawdlinux"}

	_, err := BuildRequest(svc, action, tok, params)
	if err == nil {
		t.Fatal("expected error for unresolved path param")
	}
}

func TestBuildRequest_APIKeyAuth(t *testing.T) {
	t.Parallel()
	svc := registry.Service{
		BaseURL: "https://api.example.com",
		Auth:    registry.AuthCfg{Type: "api_key", HeaderName: "X-API-Key"},
	}
	action := registry.Action{Method: "GET", Path: "/data"}
	tok := vault.Token{AccessToken: "key_123"}

	req, err := BuildRequest(svc, action, tok, nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Headers.Get("X-API-Key") != "key_123" {
		t.Fatalf("api key header = %s", req.Headers.Get("X-API-Key"))
	}
}
