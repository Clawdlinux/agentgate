/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package signer

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"sync"
	"testing"
)

type countingAEAD struct {
	cipher.AEAD
	opens int
}

func (c *countingAEAD) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	c.opens++
	return c.AEAD.Open(dst, nonce, ciphertext, additionalData)
}

func TestStoreCacheDecryptsOnceAndCloses(t *testing.T) {
	database := setupTestDB(t)
	seed, err := NewStore(database, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := seed.LoadOrCreateActive(1); err != nil {
		t.Fatal(err)
	}
	seed.Close()
	store, err := NewStore(database, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	counter := &countingAEAD{AEAD: store.gcm}
	store.gcm = counter
	for range 100 {
		if _, _, err := store.LoadOrCreateActive(1); err != nil {
			t.Fatal(err)
		}
	}
	if counter.opens != 1 {
		t.Fatalf("decrypts = %d, want 1", counter.opens)
	}
	old := store.active.priv
	store.Close()
	store.Close()
	if !bytes.Equal(old, make([]byte, len(old))) {
		t.Fatal("Close retained private key bytes")
	}
	if _, _, err := store.LoadOrCreateActive(1); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("load after Close: %v", err)
	}
	if _, _, err := store.Rotate(2); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("rotate after Close: %v", err)
	}
}

func TestStoreRotateReplacesCacheAndZeroesOldKey(t *testing.T) {
	store, err := NewStore(setupTestDB(t), testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, returned, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}
	old := store.active.priv
	rotated, _, err := store.Rotate(2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(old, make([]byte, len(old))) {
		t.Fatal("rotation retained old private key bytes")
	}
	if bytes.Equal(returned, old) {
		t.Fatal("load returned cache-owned private key bytes")
	}
	cached, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}
	if cached.KID != rotated.KID || cached.KID == first.KID {
		t.Fatal("cache did not follow rotation")
	}
	clear(priv)
	clear(cached.PublicKey)
	again, againPriv, err := store.LoadOrCreateActive(1)
	if err != nil || !bytes.Equal(again.PublicKey, rotated.PublicKey) || bytes.Equal(priv, againPriv) {
		t.Fatal("caller modified the cached key")
	}
}

func TestStoreConcurrentLoadRotateAndClose(t *testing.T) {
	store, err := NewStore(setupTestDB(t), testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 50 {
				_, priv, err := store.LoadOrCreateActive(1)
				if err != nil && !errors.Is(err, ErrStoreClosed) {
					t.Error(err)
				}
				clear(priv)
			}
		})
	}
	wg.Go(func() {
		for seq := uint64(2); seq < 12; seq++ {
			_, priv, err := store.Rotate(seq)
			if err != nil && !errors.Is(err, ErrStoreClosed) {
				t.Error(err)
			}
			clear(priv)
		}
	})
	wg.Go(func() { store.Close() })
	wg.Wait()
}
