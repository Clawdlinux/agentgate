/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package signer manages AgentGate's durable Ed25519 receipt-signing
// identity: key generation, encrypted-at-rest persistence, deterministic
// key IDs, and rotation bound to receipt sequence intervals.
package signer

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// ErrKeyUnreadable is returned when persisted key material exists but
// cannot be decrypted. Callers must fail startup rather than replace the
// key silently — a silent replacement would break every existing receipt
// chain signed under the original key.
var ErrKeyUnreadable = errors.New("signer: persisted key material is unreadable")

// ErrNoActiveKey is returned when no active signing key exists and none
// could be created.
var ErrNoActiveKey = errors.New("signer: no active key")

// ErrRotationSequence is returned when a rotation's starting sequence does
// not strictly exceed the current active key's starting sequence.
var ErrRotationSequence = errors.New("signer: rotation sequence must exceed the active key's valid_from_seq")

const kidPrefix = "ed25519:"

// KeyRecord describes one signing key's public identity and validity
// interval. It contains no private material, so it is safe to marshal and
// return directly from a verifier-facing HTTP handler.
type KeyRecord struct {
	KID           string
	PublicKey     ed25519.PublicKey
	CreatedAt     time.Time
	ValidFromSeq  uint64
	ValidUntilSeq *uint64 // nil means still active (open-ended)
}

// ComputeKID returns the deterministic key ID for a public key: bound only
// to the public key bytes, so any holder of the public key can reproduce it
// without gateway state.
func ComputeKID(pub ed25519.PublicKey) string {
	digest := sha256.Sum256(pub)
	return kidPrefix + hex.EncodeToString(digest[:8])
}

// Sign returns the Ed25519 signature of message under priv.
func Sign(priv ed25519.PrivateKey, message []byte) [64]byte {
	var signature [64]byte
	copy(signature[:], ed25519.Sign(priv, message))
	return signature
}

// Verify reports whether signature is a valid Ed25519 signature of message
// under pub. It requires only a public key — no signer-side state — so an
// independent verifier can validate a receipt using a separately trusted
// public root and key history alone.
func Verify(pub ed25519.PublicKey, message []byte, signature [64]byte) bool {
	return ed25519.Verify(pub, message, signature[:])
}
