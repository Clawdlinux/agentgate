package auth

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestUpgradedLegacyKeyValidatesWhileBcryptSlotsAreFull(t *testing.T) {
	store := newTestKeyStore(t, testDB(t), WithCacheSize(0))
	upgraded := "ag_live_" + strings.Repeat("b", 64)
	insertLegacyKey(t, store, "upgraded", upgraded)
	if _, err := store.Validate(t.Context(), upgraded); err != nil {
		t.Fatal(err)
	}
	insertLegacyKey(t, store, "pending", "ag_live_"+strings.Repeat("a", 64))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int64
	store.compareLegacy = func(_, _ []byte) error {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return bcrypt.ErrMismatchedHashAndPassword
	}
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			miss := "ag_live_" + strings.Repeat("0", 64)
			if _, err := store.Validate(t.Context(), miss); !errors.Is(err, ErrKeyNotFound) {
				t.Errorf("legacy miss: %v", err)
			}
		})
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("legacy scan did not enter bcrypt")
		}
	}
	done := make(chan error, 1)
	wg.Go(func() {
		key, err := store.Validate(t.Context(), upgraded)
		if err == nil && key.ID != "upgraded" {
			t.Error("indexed lookup returned the wrong key")
		}
		done <- err
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("upgraded key rejected during bcrypt saturation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upgraded validation blocked on bcrypt slots")
	}
	if calls.Load() != 2 {
		t.Fatal("upgraded validation reached bcrypt")
	}
}
