package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Act_RedirectPolicy(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) (string, *http.Client, <-chan http.Header)
		wantError string
	}{
		{
			name: "https downgrade blocked",
			setup: func(t *testing.T) (string, *http.Client, <-chan http.Header) {
				received := make(chan http.Header, 1)
				destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					received <- r.Header.Clone()
					w.WriteHeader(http.StatusNoContent)
				}))
				t.Cleanup(destination.Close)

				source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
				}))
				t.Cleanup(source.Close)
				return source.URL, source.Client(), received
			},
			wantError: "HTTPS to HTTP redirect refused",
		},
		{
			name: "cross host blocked",
			setup: func(t *testing.T) (string, *http.Client, <-chan http.Header) {
				received := make(chan http.Header, 1)
				destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					received <- r.Header.Clone()
					w.WriteHeader(http.StatusNoContent)
				}))
				t.Cleanup(destination.Close)

				source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
				}))
				t.Cleanup(source.Close)
				return source.URL, source.Client(), received
			},
			wantError: "cross-host redirect refused",
		},
		{
			name: "same origin allowed",
			setup: func(t *testing.T) (string, *http.Client, <-chan http.Header) {
				received := make(chan http.Header, 1)
				var source *httptest.Server
				source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/act" {
						http.Redirect(w, r, source.URL+"/redirected", http.StatusTemporaryRedirect)
						return
					}
					received <- r.Header.Clone()
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(ActResponse{Status: http.StatusOK})
				}))
				t.Cleanup(source.Close)
				return source.URL, source.Client(), received
			},
		},
		{
			name: "three hops allowed",
			setup: func(t *testing.T) (string, *http.Client, <-chan http.Header) {
				received := make(chan http.Header, 1)
				var source *httptest.Server
				source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/v1/act":
						http.Redirect(w, r, source.URL+"/hop-1", http.StatusTemporaryRedirect)
					case "/hop-1":
						http.Redirect(w, r, source.URL+"/hop-2", http.StatusTemporaryRedirect)
					case "/hop-2":
						http.Redirect(w, r, source.URL+"/hop-3", http.StatusTemporaryRedirect)
					default:
						received <- r.Header.Clone()
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(ActResponse{Status: http.StatusOK})
					}
				}))
				t.Cleanup(source.Close)
				return source.URL, source.Client(), received
			},
		},
		{
			name: "fourth hop blocked",
			setup: func(t *testing.T) (string, *http.Client, <-chan http.Header) {
				received := make(chan http.Header, 1)
				var source *httptest.Server
				source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/v1/act":
						http.Redirect(w, r, source.URL+"/hop-1", http.StatusTemporaryRedirect)
					case "/hop-1":
						http.Redirect(w, r, source.URL+"/hop-2", http.StatusTemporaryRedirect)
					case "/hop-2":
						http.Redirect(w, r, source.URL+"/hop-3", http.StatusTemporaryRedirect)
					case "/hop-3":
						http.Redirect(w, r, source.URL+"/hop-4", http.StatusTemporaryRedirect)
					default:
						received <- r.Header.Clone()
						w.WriteHeader(http.StatusNoContent)
					}
				}))
				t.Cleanup(source.Close)
				return source.URL, source.Client(), received
			},
			wantError: "more than 3 redirects refused",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseURL, httpClient, received := test.setup(t)
			client := NewClient(baseURL, "redirect-secret", WithHTTPClient(httpClient))
			_, err := client.Act(context.Background(), ActRequest{Service: "github", Action: "list_repos"})

			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Act() error = %v", err)
				}
				select {
				case headers := <-received:
					if got := headers.Get("Authorization"); got != "Bearer redirect-secret" {
						t.Fatalf("Authorization = %q", got)
					}
				default:
					t.Fatal("same-origin redirect did not reach destination")
				}
				return
			}

			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Act() error = %v, want substring %q", err, test.wantError)
			}
			if strings.Contains(err.Error(), "redirect-secret") {
				t.Fatalf("Act() error exposed API key: %v", err)
			}
			select {
			case headers := <-received:
				t.Fatalf("blocked redirect reached destination with headers: %v", headers)
			default:
			}
		})
	}
}

func TestClient_Act_ResponseBodyLimit(t *testing.T) {
	const prefix = `{"status":200,"body_text":"`
	const suffix = `"}`

	tests := []struct {
		name      string
		bodySize  int
		wantError string
	}{
		{name: "exact limit accepted", bodySize: maxResponseBodyBytes},
		{name: "over limit rejected", bodySize: maxResponseBodyBytes + 1, wantError: "response status 200 exceeds 10485760-byte limit"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(prefix + strings.Repeat("x", test.bodySize-len(prefix)-len(suffix)) + suffix))
			}))
			defer mockGateway.Close()

			client := NewClient(mockGateway.URL, "key")
			response, err := client.Act(context.Background(), ActRequest{})
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Act() error = %v, want substring %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("Act() error = %v", err)
			}
			if got := len(response.BodyText); got != test.bodySize-len(prefix)-len(suffix) {
				t.Fatalf("BodyText length = %d", got)
			}
		})
	}
}

func TestClient_Act_NonJSONErrorBodyIsSanitizedAndTruncated(t *testing.T) {
	const visiblePrefix = "badresponse"
	body := visiblePrefix + "\n\t\x00" + strings.Repeat("x", 250) + "must-not-appear"
	mockGateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(body))
	}))
	defer mockGateway.Close()

	client := NewClient(mockGateway.URL, "key")
	_, err := client.Act(context.Background(), ActRequest{})
	if err == nil {
		t.Fatal("Act() error = nil")
	}
	message := err.Error()
	if strings.ContainsAny(message, "\n\t\x00") {
		t.Fatalf("Act() error contains control characters: %q", message)
	}
	if strings.Contains(message, "must-not-appear") {
		t.Fatalf("Act() error contains unbounded response body: %q", message)
	}
	wantSnippet := visiblePrefix + strings.Repeat("x", maxErrorSnippetBytes-len(visiblePrefix))
	if !strings.Contains(message, wantSnippet) {
		t.Fatalf("Act() error = %q, want sanitized %d-byte snippet", message, maxErrorSnippetBytes)
	}
}

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
