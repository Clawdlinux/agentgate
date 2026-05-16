package vault

import (
	"errors"
	"testing"
	"time"
)

func TestGetOrRefresh_FreshToken(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken: "still-valid",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	refreshCalled := false
	tok, err := GetOrRefresh(store, "user-1", "github", func(rt string) (string, string, time.Duration, error) {
		refreshCalled = true
		return "", "", 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalled {
		t.Fatal("refresh should not be called for fresh token")
	}
	if tok.AccessToken != "still-valid" {
		t.Fatalf("got %s", tok.AccessToken)
	}
}

func TestGetOrRefresh_ExpiredToken(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken:  "old-token",
		RefreshToken: "refresh-123",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	tok, err := GetOrRefresh(store, "user-1", "github", func(rt string) (string, string, time.Duration, error) {
		if rt != "refresh-123" {
			t.Fatalf("unexpected refresh token: %s", rt)
		}
		return "new-access", "new-refresh", time.Hour, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "new-access" {
		t.Fatalf("access = %s", tok.AccessToken)
	}

	// Verify stored token was updated.
	stored, _ := store.Get("user-1", "github")
	if stored.AccessToken != "new-access" {
		t.Fatalf("stored access = %s", stored.AccessToken)
	}
}

func TestGetOrRefresh_RefreshFails(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken:  "expired",
		RefreshToken: "bad-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	_, err := GetOrRefresh(store, "user-1", "github", func(rt string) (string, string, time.Duration, error) {
		return "", "", 0, errors.New("refresh denied")
	})
	if err == nil {
		t.Fatal("expected error when refresh fails")
	}
}

func TestGetOrRefresh_NoRefreshToken(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})

	_, err := GetOrRefresh(store, "user-1", "github", nil)
	if err == nil {
		t.Fatal("expected error when no refresh token")
	}
}
