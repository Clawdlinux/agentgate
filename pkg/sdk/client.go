package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the AgentGate SDK client for agents.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// Option configures the Client.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(cl *Client) { cl.httpClient.Timeout = d }
}

// NewClient creates a new AgentGate SDK client.
//
//	client := sdk.NewClient("http://localhost:8080", "ag_live_...")
func NewClient(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ActRequest is the request payload for POST /v1/act.
type ActRequest struct {
	Service    string                 `json:"service"`
	Action     string                 `json:"action"`
	OnBehalfOf string                 `json:"on_behalf_of"`
	Params     map[string]interface{} `json:"params,omitempty"`
}

// ActResponse wraps the upstream response from the gateway.
type ActResponse struct {
	Status   int               `json:"status"`
	Body     json.RawMessage   `json:"body,omitempty"`
	BodyText string            `json:"body_text,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Meta     Meta              `json:"meta"`
	Error    string            `json:"error,omitempty"`
	Code     string            `json:"code,omitempty"`
	Message  string            `json:"message,omitempty"`
}

// Meta contains response metadata.
type Meta struct {
	LatencyMs int64 `json:"latency_ms"`
	Cached    bool  `json:"cached"`
}

// Act executes an action on a SaaS service via the gateway.
func (c *Client) Act(ctx context.Context, req ActRequest) (*ActResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("agentgate: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/act", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("agentgate: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agentgate: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
	if err != nil {
		return nil, fmt.Errorf("agentgate: read response: %w", err)
	}

	var actResp ActResponse
	if err := json.Unmarshal(respBody, &actResp); err != nil {
		// If we can't parse the response, wrap the raw body.
		return nil, &AgentGateError{
			Status:  resp.StatusCode,
			Code:    "parse_error",
			Message: fmt.Sprintf("failed to parse response: %s", string(respBody)),
		}
	}

	// Check for gateway-level errors.
	if actResp.Error != "" {
		return nil, &AgentGateError{
			Status:  resp.StatusCode,
			Code:    actResp.Code,
			Message: actResp.Error,
		}
	}

	return &actResp, nil
}

// Healthz checks if the gateway is healthy.
func (c *Client) Healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("agentgate: healthz: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("agentgate: healthz: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("agentgate: healthz returned %d", resp.StatusCode)
	}
	return nil
}

// ListServices returns all available service names.
func (c *Client) ListServices(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v1/services", nil)
	if err != nil {
		return nil, fmt.Errorf("agentgate: list services: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agentgate: list services: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Services []string `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("agentgate: parse services: %w", err)
	}
	return result.Services, nil
}
