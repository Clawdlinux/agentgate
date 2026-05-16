package sdk

import "fmt"

// AgentGateError represents an error from the gateway.
type AgentGateError struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RelinkURL string `json:"relink_url,omitempty"`
}

func (e *AgentGateError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("agentgate [%d] %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("agentgate [%d]: %s", e.Status, e.Message)
}

// IsTokenExpired returns true if the error indicates the user's token has expired.
func IsTokenExpired(err error) bool {
	if e, ok := err.(*AgentGateError); ok {
		return e.Code == "token_expired"
	}
	return false
}

// IsTokenMissing returns true if the user hasn't linked their account.
func IsTokenMissing(err error) bool {
	if e, ok := err.(*AgentGateError); ok {
		return e.Code == "token_missing"
	}
	return false
}

// IsRateLimited returns true if the request was rate limited.
func IsRateLimited(err error) bool {
	if e, ok := err.(*AgentGateError); ok {
		return e.Code == "rate_limited" || e.Status == 429
	}
	return false
}

// IsUnauthorized returns true if the API key is invalid or missing.
func IsUnauthorized(err error) bool {
	if e, ok := err.(*AgentGateError); ok {
		return e.Code == "unauthorized" || e.Status == 401
	}
	return false
}
