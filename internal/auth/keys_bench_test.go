package auth

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkValidateInvalidKey(b *testing.B) {
	for _, count := range []int{1, 200} {
		b.Run(fmt.Sprintf("keys_%d", count), func(b *testing.B) {
			store := newTestKeyStore(b, testDB(b))
			for i := 0; i < count; i++ {
				if _, _, err := store.Create(b.Context(), "agent", []string{"*"}, []string{"*"}); err != nil {
					b.Fatal(err)
				}
			}
			key := "agk_" + strings.Repeat("0", 32) + "_" + strings.Repeat("0", 64)
			b.ResetTimer()
			for b.Loop() {
				if _, err := store.Validate(b.Context(), key); err != ErrKeyNotFound {
					b.Fatalf("Validate: %v", err)
				}
			}
		})
	}
}

func BenchmarkValidateValidKey(b *testing.B) {
	for _, mode := range []string{"cache_miss", "cache_hit"} {
		b.Run(mode, func(b *testing.B) {
			var options []KeyStoreOption
			if mode == "cache_miss" {
				options = append(options, WithCacheSize(0))
			}
			store := newTestKeyStore(b, testDB(b), options...)
			_, key, err := store.Create(b.Context(), "agent", []string{"*"}, []string{"*"})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := store.Validate(b.Context(), key); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				if _, err := store.Validate(b.Context(), key); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
