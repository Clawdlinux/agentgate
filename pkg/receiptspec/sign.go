/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrSignature means a signer returned a malformed signature or a receipt
// signature did not verify.
var ErrSignature = errors.New("receipt: signature invalid")

// ErrEntryHashMismatch means a receipt's EntryHash does not match its fields.
var ErrEntryHashMismatch = errors.New("receipt: entry hash mismatch")

// Signer signs receipt entry hashes. Sign must return a 64-byte Ed25519
// signature over msg, and KID must name the key that produced it.
// Implementations can keep the private key anywhere (memory, HSM, KMS).
type Signer interface {
	KID() string
	Sign(msg []byte) ([]byte, error)
}

// ComputeKID returns the deterministic key ID for an Ed25519 public key:
// "ed25519:" plus the first 8 bytes of SHA-256(pub) in lowercase hex.
func ComputeKID(pub ed25519.PublicKey) string {
	digest := sha256.Sum256(pub)
	return "ed25519:" + hex.EncodeToString(digest[:8])
}

type ed25519Signer struct {
	kid  string
	priv ed25519.PrivateKey
}

// NewEd25519Signer returns a Signer backed by an in-memory private key.
// Its KID is ComputeKID of the matching public key.
func NewEd25519Signer(priv ed25519.PrivateKey) Signer {
	return ed25519Signer{kid: ComputeKID(priv.Public().(ed25519.PublicKey)), priv: priv}
}

func (s ed25519Signer) KID() string { return s.kid }

func (s ed25519Signer) Sign(msg []byte) ([]byte, error) { return ed25519.Sign(s.priv, msg), nil }

// Seal sets SignerKID from s, computes EntryHash, and signs it.
// Seq, PrevHash, and every other field must already be set.
func Seal(r Receipt, s Signer) (Receipt, error) {
	r.SignerKID = s.KID()
	hash, err := ComputeEntryHash(r)
	if err != nil {
		return Receipt{}, err
	}
	sig, err := s.Sign(hash[:])
	if err != nil {
		return Receipt{}, fmt.Errorf("receipt: sign: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return Receipt{}, fmt.Errorf("%w: signer returned %d bytes", ErrSignature, len(sig))
	}
	r.EntryHash = hash
	copy(r.Signature[:], sig)
	return r, nil
}

// VerifyReceipt checks one receipt in isolation: fields are valid, EntryHash
// matches, and Signature verifies under pub. It does not check chain links,
// the signer's validity window, or that pub belongs to SignerKID.
func VerifyReceipt(r Receipt, pub ed25519.PublicKey) error {
	hash, err := ComputeEntryHash(r)
	if err != nil {
		return err
	}
	if hash != r.EntryHash {
		return ErrEntryHashMismatch
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, hash[:], r.Signature[:]) {
		return ErrSignature
	}
	return nil
}
