package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey string

const agentKeyContextKey contextKey = "agent_key"

// AgentKeyFromContext retrieves the authenticated AgentKey from the request context.
func AgentKeyFromContext(ctx context.Context) (*AgentKey, bool) {
	key, ok := ctx.Value(agentKeyContextKey).(*AgentKey)
	return key, ok
}

// Middleware returns HTTP middleware that validates the X-AgentGate-Key or
// Authorization: Bearer header. On success, stores the AgentKey in context.
func Middleware(store *KeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := extractKey(r)
			if key == "" {
				writeAuthError(w, http.StatusUnauthorized, "missing API key", "unauthorized")
				return
			}

			agentKey, err := store.Validate(r.Context(), key)
			if err != nil {
				switch err {
				case ErrKeyRevoked:
					writeAuthError(w, http.StatusUnauthorized, "API key has been revoked", "key_revoked")
				case ErrKeyNotFound:
					writeAuthError(w, http.StatusUnauthorized, "invalid API key", "unauthorized")
				default:
					writeAuthError(w, http.StatusInternalServerError, "authentication error", "internal")
				}
				return
			}

			ctx := context.WithValue(r.Context(), agentKeyContextKey, agentKey)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractKey(r *http.Request) string {
	// Check X-AgentGate-Key header first.
	if key := r.Header.Get("X-AgentGate-Key"); key != "" {
		return key
	}
	// Fall back to Authorization: Bearer.
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func writeAuthError(w http.ResponseWriter, status int, msg, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": msg,
		"code":  code,
	})
}
