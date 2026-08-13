/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package signer

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// KEY-04: GET /v1/receipts/pubkey publishes active and historical public
// keys without private material.
func TestPubkeyHandler_ExposesKeysWithoutPrivateMaterial(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Rotate(50); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/receipts/pubkey", nil)
	recorder := httptest.NewRecorder()
	PubkeyHandler(store)(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	body := recorder.Body.String()
	if strings.Contains(strings.ToLower(body), "private") {
		t.Fatalf("response body mentions private material: %s", body)
	}

	var response pubkeyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Keys) != 2 {
		t.Fatalf("key count = %d, want 2", len(response.Keys))
	}

	var foundFirst bool
	for _, key := range response.Keys {
		if key.KID != first.KID {
			continue
		}
		foundFirst = true
		if key.PublicKeyHex != hex.EncodeToString(first.PublicKey) {
			t.Fatalf("public_key_hex = %s, want %s", key.PublicKeyHex, hex.EncodeToString(first.PublicKey))
		}
	}
	if !foundFirst {
		t.Fatal("first key missing from response")
	}
}

func TestPubkeyHandler_RejectsNonGet(t *testing.T) {
	t.Parallel()
	db := setupTestDB(t)
	store, err := NewStore(db, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/receipts/pubkey", nil)
	recorder := httptest.NewRecorder()
	PubkeyHandler(store)(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}
