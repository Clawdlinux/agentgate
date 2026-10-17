package vault

import (
	"testing"
	"time"
)

func BenchmarkVaultGet(b *testing.B) {
	store, err := NewSQLiteStore(setupTestDB(b), testKey())
	if err != nil {
		b.Fatal(err)
	}
	if err := store.Put("user", "service", Token{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{"read", "write"},
	}); err != nil {
		b.Fatal(err)
	}
	b.Run("SQLite", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := store.Get("user", "service"); err != nil {
				b.Fatal(err)
			}
		}
	})
	cached, err := NewCachedStore(store)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Cached", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := cached.Get("user", "service"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
