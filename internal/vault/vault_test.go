package vault

import (
	"crypto/rand"
	"errors"
	"testing"
	"time"
)

func testKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

func TestMemoryStore_PutGet(t *testing.T) {
	t.Parallel()
	store, err := NewMemoryStore(testKey())
	if err != nil {
		t.Fatal(err)
	}

	tok := Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{"repo", "read:org"},
	}

	if err := store.Put("user-1", "github", tok); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get("user-1", "github")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.AccessToken != "access-123" {
		t.Fatalf("access_token = %s", got.AccessToken)
	}
	if got.RefreshToken != "refresh-456" {
		t.Fatalf("refresh_token = %s", got.RefreshToken)
	}
	if len(got.Scopes) != 2 {
		t.Fatalf("scopes = %v", got.Scopes)
	}
}

func TestMemoryStore_NotFound(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_, err := store.Get("nobody", "nothing")
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "x"})
	_ = store.Delete("user-1", "github")

	_, err := store.Get("user-1", "github")
	if !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestMemoryStore_ListServices(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "a"})
	_ = store.Put("user-1", "stripe", Token{AccessToken: "b"})
	_ = store.Put("user-2", "slack", Token{AccessToken: "c"})

	services, err := store.ListServices("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %v, want 2 entries", services)
	}
}

func TestMemoryStore_EncryptionAtRest(t *testing.T) {
	t.Parallel()
	store, _ := NewMemoryStore(testKey())

	_ = store.Put("user-1", "github", Token{AccessToken: "secret-token-value"})

	// Verify the stored bytes are NOT the plaintext.
	store.mu.RLock()
	raw := store.tokens["user-1:github"]
	store.mu.RUnlock()

	for i := 0; i < len(raw)-17; i++ {
		if string(raw[i:i+18]) == "secret-token-value" {
			t.Fatal("plaintext access token found in stored bytes — encryption failed")
		}
	}
}

func TestToken_IsExpired(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		tok     Token
		expired bool
	}{
		{"no expiry", Token{}, false},
		{"future", Token{ExpiresAt: time.Now().Add(time.Hour)}, false},
		{"past", Token{ExpiresAt: time.Now().Add(-time.Hour)}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.tok.IsExpired() != tc.expired {
				t.Fatalf("IsExpired() = %v, want %v", tc.tok.IsExpired(), tc.expired)
			}
		})
	}
}

func TestMemoryStore_BadKeyLength(t *testing.T) {
	t.Parallel()
	_, err := NewMemoryStore([]byte("too-short"))
	if err == nil {
		t.Fatal("expected error for bad key length")
	}
}
