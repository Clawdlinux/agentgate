package gateway

import "github.com/Clawdlinux/agentgate/internal/auth"

// tenantIdentity bounds each key to 64 tenant identities without a separate tracking cache.
// Wildcard keys share one key-scoped tenant across all requested principals.
func tenantIdentity(key *auth.AgentKey, principal string) string {
	fallback := "key:" + key.ID
	for _, allowed := range key.AllowedUsers {
		if allowed == "*" {
			return fallback
		}
	}
	limit := 64
	if len(key.AllowedUsers) > limit {
		limit-- // Reserve one identity for the overflow bucket.
	}
	for _, allowed := range key.AllowedUsers[:min(limit, len(key.AllowedUsers))] {
		if allowed == principal {
			return "human:" + principal
		}
	}
	return fallback
}
