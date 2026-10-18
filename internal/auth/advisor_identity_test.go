package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestAdvisorIdentityCannotAuthenticate(t *testing.T) {
	const reserved = "agentgate:advisor"
	for _, legacy := range []bool{false, true} {
		store := newTestKeyStore(t, testDB(t))
		var plaintext string
		if legacy {
			plaintext = "ag_live_" + strings.Repeat("b", 64)
			insertLegacyKey(t, store, reserved, plaintext)
		} else {
			key, secret, err := store.Create(t.Context(), "test", []string{"*"}, []string{"*"})
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := hex.DecodeString(key.ID)
			if err != nil || len(decoded) != 16 || key.ID == reserved {
				t.Fatal("key ID format is not isolated")
			}
			if _, err := store.db.ExecContext(t.Context(), "UPDATE agent_keys SET id=? WHERE id=?", reserved, key.ID); err != nil {
				t.Fatal(err)
			}
			plaintext = secret
		}
		if key, err := store.Validate(t.Context(), plaintext); !errors.Is(err, ErrKeyNotFound) {
			t.Fatalf("reserved identity authenticated: key=%+v err=%v", key, err)
		}
	}

}

func TestCachedAdvisorIdentityCannotAuthenticate(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	plaintext, _, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(plaintext))
	_, generation := store.cached(digest)
	store.cacheKey(digest, &AgentKey{ID: "agentgate:advisor"}, generation)
	if key, _ := store.cached(digest); key == nil {
		t.Fatal("test cache was not populated")
	}
	if _, err := store.Validate(t.Context(), plaintext); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("reserved cached identity authenticated: %v", err)
	}
}
