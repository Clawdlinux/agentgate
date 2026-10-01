/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Reason values are stable ASCII and never derived from a receipt's own
// fields, so they are safe to print without leaking receipted data.
const (
	ReasonSequenceGap          = "sequence_gap_or_not_genesis_anchored"
	ReasonPrevHashMismatch     = "prev_hash_mismatch"
	ReasonInvalidReceiptFields = "invalid_receipt_fields"
	ReasonEntryHashMismatch    = "entry_hash_mismatch"
	ReasonSignatureInvalid     = "signature_invalid"
	ReasonSignerInactiveAtSeq  = "signer_kid_inactive_at_seq"
	ReasonExpectedHeadMismatch = "expected_head_mismatch"
)

var (
	// ErrNoTrustedKeys means no trusted keys were supplied. This is a
	// configuration error, not a tamper finding.
	ErrNoTrustedKeys = errors.New("receipt: no trusted keys supplied")
	// ErrEmptyChain means the source supplied zero receipts.
	ErrEmptyChain = errors.New("receipt: empty receipt chain")
	// ErrUnknownSignerKID means a receipt names a signer_kid absent from the
	// trust set. A verifier with a stale trust file cannot tell this apart
	// from an attacker's key, so it is reported as a configuration error.
	ErrUnknownSignerKID = errors.New("receipt: unknown signer_kid")
)

// TrustedKey is one entry in a verifier's trust set: a public key plus its
// inclusive [ValidFromSeq, ValidUntilSeq] sequence window.
//
// Key rotation is not signed by the previous key. A trust set must contain
// every key that signed a receipt in the checked range, obtained through a
// trusted channel. A receipt signed outside its key's window fails.
type TrustedKey struct {
	KID           string
	PublicKey     ed25519.PublicKey
	ValidFromSeq  uint64
	ValidUntilSeq *uint64 // nil means open-ended (still active when the trust file was saved)
}

type trustedKeyJSON struct {
	KID           string  `json:"kid"`
	PublicKeyHex  string  `json:"public_key_hex"`
	ValidFromSeq  uint64  `json:"valid_from_seq"`
	ValidUntilSeq *uint64 `json:"valid_until_seq"`
}

type trustFileJSON struct {
	Keys []trustedKeyJSON `json:"keys"`
}

// LoadTrustedKeys parses a trust file shaped like the gateway's
// GET /v1/receipts/pubkey response: {"keys": [{"kid", "public_key_hex",
// "valid_from_seq", "valid_until_seq"}]}. It never fetches anything.
func LoadTrustedKeys(data []byte) ([]TrustedKey, error) {
	var file trustFileJSON
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("receipt: parse trust file: %w", err)
	}
	if len(file.Keys) == 0 {
		return nil, ErrNoTrustedKeys
	}
	out := make([]TrustedKey, 0, len(file.Keys))
	for _, k := range file.Keys {
		pub, err := hex.DecodeString(k.PublicKeyHex)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("receipt: trust file key %s: public_key must be %d bytes hex", k.KID, ed25519.PublicKeySize)
		}
		out = append(out, TrustedKey{
			KID:           k.KID,
			PublicKey:     ed25519.PublicKey(pub),
			ValidFromSeq:  k.ValidFromSeq,
			ValidUntilSeq: k.ValidUntilSeq,
		})
	}
	return out, nil
}

// ExpectedHead pins a separately trusted (seq, entry_hash). Without one, a
// verifier cannot detect a deleted suffix, only internal inconsistency.
type ExpectedHead struct {
	Seq       uint64
	EntryHash [32]byte
}

// Anchor is the committed (seq, entry_hash) immediately before the first
// checked receipt. The zero value is genesis.
type Anchor struct {
	Seq       uint64
	EntryHash [32]byte
}

// ParseExpectedHead parses "SEQ:HEXHASH".
func ParseExpectedHead(s string) (ExpectedHead, error) {
	seqStr, hashStr, ok := strings.Cut(s, ":")
	if !ok {
		return ExpectedHead{}, errors.New("receipt: --expected-head must be SEQ:HEXHASH")
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		return ExpectedHead{}, fmt.Errorf("receipt: --expected-head seq: %w", err)
	}
	decoded, err := hex.DecodeString(hashStr)
	if err != nil || len(decoded) != 32 {
		return ExpectedHead{}, errors.New("receipt: --expected-head hash must be 32 bytes hex")
	}
	var out ExpectedHead
	out.Seq = seq
	copy(out.EntryHash[:], decoded)
	return out, nil
}

// VerifyResult reports one verification. Every field is a count, a sequence
// number, a hash, or a Reason constant, so it is safe to print in full.
type VerifyResult struct {
	OK            bool
	TotalReceipts int
	VerifiedCount int
	FailedAtSeq   uint64 // 0 only when OK and no failure occurred
	Reason        string // one of the Reason* constants; empty when OK
	HeadSeq       uint64
	HeadEntryHash [32]byte
	// Complete is true only when an ExpectedHead was supplied and matched the
	// final receipt. False means completeness was not claimed, not tampering.
	Complete bool
}

// VerifyChain checks receipts, ordered by ascending Seq, against trustedKeys
// starting from anchor. A returned error means the input could not be
// checked at all (no keys, no receipts, unknown signer_kid). A result with
// OK == false means verification ran and found a mismatch.
//
// If expectedHead is non-nil, the final receipt must match it exactly.
func VerifyChain(receipts []Receipt, trustedKeys []TrustedKey, anchor Anchor, expectedHead *ExpectedHead) (VerifyResult, error) {
	if len(trustedKeys) == 0 {
		return VerifyResult{}, ErrNoTrustedKeys
	}
	if len(receipts) == 0 {
		return VerifyResult{}, ErrEmptyChain
	}

	byKID := make(map[string]TrustedKey, len(trustedKeys))
	for _, k := range trustedKeys {
		byKID[k.KID] = k
	}

	result := VerifyResult{TotalReceipts: len(receipts)}
	wantSeq := anchor.Seq + 1
	prevHash := anchor.EntryHash
	for i := range receipts {
		r := receipts[i]

		if r.Seq != wantSeq+uint64(i) {
			result.FailedAtSeq = r.Seq
			result.Reason = ReasonSequenceGap
			return result, nil
		}
		if r.PrevHash != prevHash {
			result.FailedAtSeq = r.Seq
			result.Reason = ReasonPrevHashMismatch
			return result, nil
		}

		key, ok := byKID[r.SignerKID]
		if !ok {
			return VerifyResult{}, fmt.Errorf("%w: %s", ErrUnknownSignerKID, r.SignerKID)
		}
		if r.Seq < key.ValidFromSeq || (key.ValidUntilSeq != nil && r.Seq > *key.ValidUntilSeq) {
			result.FailedAtSeq = r.Seq
			result.Reason = ReasonSignerInactiveAtSeq
			return result, nil
		}

		entryHash, err := ComputeEntryHash(r)
		if err != nil {
			result.FailedAtSeq = r.Seq
			result.Reason = ReasonInvalidReceiptFields
			return result, nil
		}
		if entryHash != r.EntryHash {
			result.FailedAtSeq = r.Seq
			result.Reason = ReasonEntryHashMismatch
			return result, nil
		}
		if !ed25519.Verify(key.PublicKey, entryHash[:], r.Signature[:]) {
			result.FailedAtSeq = r.Seq
			result.Reason = ReasonSignatureInvalid
			return result, nil
		}

		prevHash = entryHash
		result.VerifiedCount++
		result.HeadSeq = r.Seq
		result.HeadEntryHash = entryHash
	}

	result.OK = true
	if expectedHead != nil {
		if result.HeadSeq == expectedHead.Seq && result.HeadEntryHash == expectedHead.EntryHash {
			result.Complete = true
		} else {
			result.OK = false
			result.FailedAtSeq = expectedHead.Seq
			result.Reason = ReasonExpectedHeadMismatch
		}
	}
	return result, nil
}

// ManifestError wraps a manifest verification failure from VerifyBundle.
// Unlike VerifyChain's errors it is a tamper finding, not a config error.
type ManifestError struct {
	Err error
}

// Error returns the wrapped manifest error's message.
func (e *ManifestError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying manifest error.
func (e *ManifestError) Unwrap() error { return e.Err }

// VerifyBundle verifies a parsed source the way agentgate-verify does.
// trusted overrides b.Keys when non-empty; otherwise the embedded keys are
// the trust set. A manifest, if present, must verify (else *ManifestError)
// and then supplies the anchor and, unless expected is set, the expected head.
func VerifyBundle(b Bundle, trusted []TrustedKey, expected *ExpectedHead) (VerifyResult, error) {
	if len(trusted) == 0 {
		trusted = b.Keys
	}
	anchor := Anchor{}
	if b.Manifest != nil {
		if err := VerifyManifest(*b.Manifest, trusted, b.Keys); err != nil {
			return VerifyResult{}, &ManifestError{Err: err}
		}
		anchor = Anchor{Seq: b.Manifest.AnchorSeq, EntryHash: b.Manifest.AnchorHash}
		if expected == nil {
			expected = &ExpectedHead{Seq: b.Manifest.ResolvedTo, EntryHash: b.Manifest.LastEntryHash}
		}
	}
	return VerifyChain(b.Receipts, trusted, anchor, expected)
}
