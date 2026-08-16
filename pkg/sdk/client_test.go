package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Act_Success(t *testing.T) {
	t.Parallel()

	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/act" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("auth = %s", r.Header.Get("Authorization"))
		}

		var req ActRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Service != "github" {
			t.Fatalf("service = %s", req.Service)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ActResponse{
			Status: 200,
			Body:   json.RawMessage(`[{"id":1}]`),
		})
	}))
	defer mockGateway.Close()

	client := NewClient(mockGateway.URL, "test-key")
	resp, err := client.Act(context.Background(), ActRequest{
		Service:    "github",
		Action:     "list_repos",
		OnBehalfOf: "user-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Fatalf("status = %d", resp.Status)
	}
}

func TestClient_Act_Error(t *testing.T) {
	t.Parallel()

	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid API key",
			"code":  "unauthorized",
		})
	}))
	defer mockGateway.Close()

	client := NewClient(mockGateway.URL, "bad-key")
	_, err := client.Act(context.Background(), ActRequest{
		Service:    "github",
		Action:     "list_repos",
		OnBehalfOf: "user-42",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got: %v", err)
	}
}

func TestClient_Healthz(t *testing.T) {
	t.Parallel()

	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer mockGateway.Close()

	client := NewClient(mockGateway.URL, "key")
	if err := client.Healthz(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClient_ListServices(t *testing.T) {
	t.Parallel()

	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string][]string{
			"services": {"github", "stripe", "slack"},
		})
	}))
	defer mockGateway.Close()

	client := NewClient(mockGateway.URL, "key")
	services, err := client.ListServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 3 {
		t.Fatalf("services = %v", services)
	}
}

func TestClient_Helpers(t *testing.T) {
	t.Parallel()

	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ActRequest
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ActResponse{
			Status: 200,
			Body:   json.RawMessage(`{"ok":true}`),
		})
	}))
	defer mockGateway.Close()

	client := NewClient(mockGateway.URL, "key")
	ctx := context.Background()

	if _, err := client.Stripe(ctx, "user-1", "list_invoices", nil); err != nil {
		t.Fatalf("stripe: %v", err)
	}
	if _, err := client.GitHub(ctx, "user-1", "list_repos", nil); err != nil {
		t.Fatalf("github: %v", err)
	}
	if _, err := client.Slack(ctx, "user-1", "list_channels", nil); err != nil {
		t.Fatalf("slack: %v", err)
	}
	if _, err := client.GoogleWorkspace(ctx, "user-1", "list_labels", nil); err != nil {
		t.Fatalf("google workspace: %v", err)
	}
}

func TestErrorHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		check  func(error) bool
		expect bool
	}{
		{"token expired", &AgentGateError{Code: "token_expired"}, IsTokenExpired, true},
		{"token missing", &AgentGateError{Code: "token_missing"}, IsTokenMissing, true},
		{"rate limited", &AgentGateError{Code: "rate_limited"}, IsRateLimited, true},
		{"unauthorized", &AgentGateError{Code: "unauthorized"}, IsUnauthorized, true},
		{"429 status", &AgentGateError{Status: 429}, IsRateLimited, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.check(tc.err) != tc.expect {
				t.Fatalf("check(%v) = %v, want %v", tc.err, !tc.expect, tc.expect)
			}
		})
	}
}
