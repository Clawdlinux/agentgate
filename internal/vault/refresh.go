package vault

import (
	"fmt"
	"time"
)

// RefreshFunc is called when a token needs refreshing. It takes the current
// refresh token and returns new access/refresh tokens and expiry.
type RefreshFunc func(refreshToken string) (newAccess, newRefresh string, expiresIn time.Duration, err error)

// GetOrRefresh retrieves a token and auto-refreshes if it expires within
// the given buffer period (default 5 minutes). If refresh fails, returns
// an error indicating the user must re-link.
func GetOrRefresh(store Store, userID, service string, refreshFn RefreshFunc) (Token, error) {
	tok, err := store.Get(userID, service)
	if err != nil {
		return Token{}, err
	}

	// If no expiry set or not expiring soon, return as-is.
	if tok.ExpiresAt.IsZero() || time.Until(tok.ExpiresAt) > 5*time.Minute {
		return tok, nil
	}

	// Token is expired or expiring soon — attempt refresh.
	if tok.RefreshToken == "" {
		return Token{}, fmt.Errorf("vault: token for %s:%s expired and no refresh token available — user must re-link", userID, service)
	}

	if refreshFn == nil {
		return Token{}, fmt.Errorf("vault: token for %s:%s expired and no refresh function configured", userID, service)
	}

	newAccess, newRefresh, expiresIn, err := refreshFn(tok.RefreshToken)
	if err != nil {
		return Token{}, fmt.Errorf("vault: refresh failed for %s:%s — user must re-link: %w", userID, service, err)
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
