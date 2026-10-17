package vault

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RefreshFunc is called when a token needs refreshing. It takes the current
// refresh token and returns new access/refresh tokens and expiry.
type RefreshFunc func(refreshToken string) (newAccess, newRefresh string, expiresIn time.Duration, err error)

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
			call.token, call.err = refreshToken(store, userID, service, refreshFn)
			refreshFlights.Lock()
			delete(refreshFlights.calls, key)
			close(call.done)
			refreshFlights.Unlock()
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

func refreshToken(store Store, userID, service string, refreshFn RefreshFunc) (Token, error) {
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

	newAccess, newRefresh, expiresIn, err := refreshFn(tok.RefreshToken)
	if err != nil {
		return Token{}, fmt.Errorf("vault: refresh failed for %s:%s. User must re-link: %w", userID, service, err)
	}

	tok.AccessToken = newAccess
	if newRefresh != "" {
		tok.RefreshToken = newRefresh
	}
	tok.ExpiresAt = time.Now().Add(expiresIn)

	if err := store.Put(userID, service, tok); err != nil {
		return Token{}, fmt.Errorf("vault: store refreshed token: %w", err)
	}

	return tok, nil
}
