package vault

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DefaultRefreshTimeout sets the shared OAuth exchange deadline.
const DefaultRefreshTimeout = 15 * time.Second

// RefreshFunc exchanges a refresh token. It must honor context cancellation.
type RefreshFunc func(ctx context.Context, refreshToken string) (newAccess, newRefresh string, expiresIn time.Duration, err error)

type refreshKey struct {
	store Store
	credentialKey
}

type refreshCall struct {
	done  chan struct{}
	token Token
	err   error
}

var refreshFlights = struct {
	sync.Mutex
	calls map[refreshKey]*refreshCall
}{calls: make(map[refreshKey]*refreshCall)}

// GetOrRefresh refreshes tokens expiring within 5 minutes.
// Concurrent callers sharing a store and token pair share one refresh.
// Cancellation stops only the caller's wait, not the shared refresh.
func GetOrRefresh(ctx context.Context, store Store, userID, service string, refreshFn RefreshFunc) (Token, error) {
	return getOrRefresh(ctx, store, userID, service, refreshFn, DefaultRefreshTimeout)
}

func getOrRefresh(ctx context.Context, store Store, userID, service string, refreshFn RefreshFunc, timeout time.Duration) (Token, error) {
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	tok, err := store.Get(userID, service)
	if err != nil {
		return Token{}, err
	}
	if tok.ExpiresAt.IsZero() || time.Until(tok.ExpiresAt) > 5*time.Minute {
		return tok, nil
	}

	key := refreshKey{store: store, credentialKey: credentialKey{userID, service}}
	refreshFlights.Lock()
	call, exists := refreshFlights.calls[key]
	if !exists {
		call = &refreshCall{done: make(chan struct{})}
		refreshFlights.calls[key] = call
		go func() {
			defer func() {
				if recover() != nil {
					call.token, call.err = Token{}, fmt.Errorf("vault: refresh panicked")
					slog.Error("vault refresh panicked")
				}
				refreshFlights.Lock()
				delete(refreshFlights.calls, key)
				close(call.done)
				refreshFlights.Unlock()
			}()
			refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			defer cancel()
			call.token, call.err = refreshToken(refreshCtx, store, userID, service, refreshFn)
		}()
	}
	refreshFlights.Unlock()

	select {
	case <-ctx.Done():
		return Token{}, ctx.Err()
	case <-call.done:
		return cloneToken(call.token), call.err
	}
}

func refreshToken(ctx context.Context, store Store, userID, service string, refreshFn RefreshFunc) (Token, error) {
	// Re-read after joining the flight. Another refresh may have finished after the caller's first read.
	tok, err := store.Get(userID, service)
	if err != nil {
		return Token{}, err
	}

	// If no expiry set or not expiring soon, return as-is.
	if tok.ExpiresAt.IsZero() || time.Until(tok.ExpiresAt) > 5*time.Minute {
		return tok, nil
	}

	// Token is expired or expiring soon. Attempt refresh.
	if tok.RefreshToken == "" {
		return Token{}, fmt.Errorf("vault: token for %s:%s expired and no refresh token available. User must re-link", userID, service)
	}

	if refreshFn == nil {
		return Token{}, fmt.Errorf("vault: token for %s:%s expired and no refresh function configured", userID, service)
	}

	expectedRefresh := tok.RefreshToken
	newAccess, newRefresh, expiresIn, err := refreshFn(ctx, expectedRefresh)
	if err != nil {
		return Token{}, fmt.Errorf("vault: refresh failed for %s:%s. User must re-link: %w", userID, service, err)
	}
	tok.AccessToken = newAccess
	if newRefresh != "" {
		tok.RefreshToken = newRefresh
	}
	tok.ExpiresAt = time.Now().Add(expiresIn)

	attempts := 1
	rotated := newRefresh != "" && newRefresh != expectedRefresh
	if rotated {
		attempts = 3
	}
	var writeErr error
	for range attempts {
		updated, err := store.CompareAndPut(userID, service, expectedRefresh, tok)
		if err != nil {
			writeErr = err
			continue
		}
		if updated {
			return tok, nil
		}
		current, err := store.Get(userID, service)
		if err != nil {
			return Token{}, err
		}
		if current.IsExpired() {
			return Token{}, fmt.Errorf("vault: credential changed during refresh and replacement is expired")
		}
		return current, nil
	}
	slog.Error("vault refresh persistence failed", "user", userID, "service", service, "attempts", attempts)
	if rotated {
		return tok, nil
	}
	return Token{}, fmt.Errorf("vault: store refreshed token: %w", writeErr)
}
