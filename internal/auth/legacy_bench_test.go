package auth

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkValidateLegacyMiss(b *testing.B) {
	for _, count := range []int{1, 200} {
		for _, state := range []string{"unupgraded", "upgraded", "revoked"} {
			b.Run(fmt.Sprintf("keys_%d/%s", count, state), func(b *testing.B) {
				store := newTestKeyStore(b, testDB(b))
				for i := 0; i < count; i++ {
					id := fmt.Sprint(i)
					insertLegacyKey(b, store, id, "ag_live_"+strings.Repeat("a", 64))
					if state == "upgraded" {
						digest := sha256.Sum256([]byte(id))
						if _, err := store.db.ExecContext(b.Context(),
							"UPDATE agent_keys SET legacy_lookup = ? WHERE id = ?", digest[:], id); err != nil {
							b.Fatal(err)
						}
					}
					if state == "revoked" {
						if err := store.Revoke(b.Context(), id); err != nil {
							b.Fatal(err)
						}
					}
				}
				miss := "ag_live_" + strings.Repeat("0", 64)
				for b.Loop() {
					if _, err := store.Validate(b.Context(), miss); err != ErrKeyNotFound {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
