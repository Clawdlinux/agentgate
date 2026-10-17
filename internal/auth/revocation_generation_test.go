package auth

import (
	"strings"
	"testing"
	"time"
)

func TestRevocationGenerationSkipsInFlightCacheInsertion(t *testing.T) {
	store := newTestKeyStore(t, testDB(t))
	modern, _, err := store.Create(t.Context(), "modern", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy := "ag_live_" + strings.Repeat("a", 64)
	insertLegacyKey(t, store, "legacy", legacy)
	entered := make(chan struct{})
	release := make(chan struct{})
	store.compareLegacy = func(_, _ []byte) error {
		close(entered)
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.Validate(t.Context(), legacy)
		done <- err
	}()
	defer func() {
		close(release)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("legacy Validate: %v", err)
			}
			store.cacheMu.Lock()
			cached := len(store.cache)
			store.cacheMu.Unlock()
			if cached != 0 {
				t.Error("in-flight validation repopulated the cache after revocation")
			}
		case <-time.After(2 * time.Second):
			t.Error("legacy validation did not finish")
		}
	}()
	<-entered
	revoked := make(chan error, 1)
	go func() { revoked <- store.Revoke(t.Context(), modern.ID) }()
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revocation blocked on an in-flight validation")
	}
	store.cacheMu.Lock()
	generation := store.cacheGeneration
	store.cacheMu.Unlock()
	if generation == 0 {
		t.Fatal("revocation did not advance the cache generation")
	}
}
