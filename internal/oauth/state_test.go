package oauth

import (
	"crypto/rand"
	"strings"
	"testing"
)

func testKey() []byte {
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

func TestState_RoundTrip(t *testing.T) {
	t.Parallel()
	key := testKey()

	state, err := EncryptState("user-42", "stripe", key)
	if err != nil {
		t.Fatal(err)
	}

	userID, service, err := DecryptState(state, key)
	if err != nil {
		t.Fatal(err)
	}
	if userID != "user-42" {
		t.Fatalf("userID = %s", userID)
	}
	if service != "stripe" {
		t.Fatalf("service = %s", service)
	}
}

func TestState_WrongKey(t *testing.T) {
	t.Parallel()
	key1 := testKey()
	key2 := testKey()

	state, _ := EncryptState("user-42", "stripe", key1)
	_, _, err := DecryptState(state, key2)
	if err == nil {
		t.Fatal("expected error with wrong key")
	}
}

func TestState_Tampered(t *testing.T) {
	t.Parallel()
	key := testKey()

	state, _ := EncryptState("user-42", "stripe", key)
	// Tamper with the state.
	tampered := state[:len(state)-2] + "XX"
	_, _, err := DecryptState(tampered, key)
	if err == nil {
		t.Fatal("expected error with tampered state")
	}
}

func TestState_InvalidBase64(t *testing.T) {
	t.Parallel()
	key := testKey()
	_, _, err := DecryptState("not-valid-base64!!!", key)
	if err == nil {
		t.Fatal("expected error with invalid base64")
	}
}

func TestState_URLSafe(t *testing.T) {
	t.Parallel()
	key := testKey()
	state, _ := EncryptState("user-42", "stripe", key)
	// URL-safe base64 should not contain + or /
	if strings.ContainsAny(state, "+/") {
		t.Fatalf("state contains non-URL-safe chars: %s", state)
	}
}
