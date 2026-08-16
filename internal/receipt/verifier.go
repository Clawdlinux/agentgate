/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Clawdlinux/agentgate/internal/signer"
)

// Reason values are stable, ASCII, and never derived from a receipt's own
// fields — they are safe to print without leaking human_principal,
// agent_key_id, or any other receipted field (VER-10).
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
	// ErrNoTrustedKeys means the trust file supplied no keys at all — a
	// configuration error, not a tamper finding (VER-09).
	ErrNoTrustedKeys = errors.New("receipt: no trusted keys supplied")
	// ErrEmptyChain means the source supplied zero receipts (VER-09).
	ErrEmptyChain = errors.New("receipt: empty receipt chain")
	// ErrUnknownSignerKID means a receipt names a signer_kid absent from
	// the trust file. This is a configuration/input error, not a tamper
	// finding, because a verifier holding the wrong or incomplete trust
	// file cannot distinguish "attacker's key" from "my trust file is
	// stale" (VER-09).
	ErrUnknownSignerKID = errors.New("receipt: unknown signer_kid")
)

// TrustedKey is one entry in an auditor's offline trust file: the public
// half of a signer key plus its validity interval, no private material.
// Its shape mirrors internal/signer.KeyRecord's public fields, so a trust
// file can be built directly from GET /v1/receipts/pubkey's response.
//
// Trust model note: internal/signer (Phase 3) does not sign a rotation
// transition between keys — every KEY-0X requirement is satisfied without
// one. VerifyChain therefore cannot cryptographically chain trust from one
// pinned root key to later rotated keys. Instead, the auditor's trust file
// must contain every key that has ever signed a receipt in the range being
// checked, obtained once through a trusted channel. "Validates historical
// key transitions" (VER-03) is satisfied by binding each receipt's
// signer_kid to that key's declared [ValidFromSeq, ValidUntilSeq] sequence
// interval (inclusive; a nil ValidUntilSeq means still active) — a
// receipt signed by a key outside its own validity window fails
// verification — rather than by verifying a signed transition payload
// that this milestone's signer never produces.
type TrustedKey struct {
	KID           string
	PublicKey     ed25519.PublicKey
	ValidFromSeq  uint64
	ValidUntilSeq *uint64 // nil means open-ended (still active when the trust file was saved)
}

type trustedKeyJSON struct {
	KID           string  `json:"kid"`
	PublicKey     string  `json:"public_key"`
	ValidFromSeq  uint64  `json:"valid_from_seq"`
	ValidUntilSeq *uint64 `json:"valid_until_seq"`
}

// LoadTrustedKeys parses a JSON array of trusted signer keys — the same
// shape internal/signer.PubkeyHandler serves, saved once through a trusted
// channel and never fetched over the network by the offline verifier.
func LoadTrustedKeys(data []byte) ([]TrustedKey, error) {
	var raw []trustedKeyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("receipt: parse trust file: %w", err)
	}
	if len(raw) == 0 {
		return nil, ErrNoTrustedKeys
	}
	out := make([]TrustedKey, 0, len(raw))
	for _, k := range raw {
		pub, err := hex.DecodeString(k.PublicKey)
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

// ExpectedHead pins a separately trusted (seq, entry_hash) pair. Supplying
// one lets VerifyChain claim the checked range is complete rather than
// merely internally consistent: an internally consistent chain cannot by
// itself detect a deleted, unsigned suffix — a shorter chain that simply
// stops early looks identical to a complete one without an independently
// known head to compare against.
type ExpectedHead struct {
	Seq       uint64
	EntryHash [32]byte
}

// ParseExpectedHead parses the --expected-head flag value "SEQ:HEXHASH".
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

// VerifyResult reports the outcome of one VerifyChain call. It is safe to
// print in full: every field is either a count, a sequence number, or one
// of the fixed Reason* constants (VER-10).
type VerifyResult struct {
	OK            bool
	TotalReceipts int
	VerifiedCount int
	FailedAtSeq   uint64 // 0 only when OK and no failure occurred
	Reason        string // one of the Reason* constants; empty when OK
	HeadSeq       uint64
	HeadEntryHash [32]byte
	// Complete is true only when an ExpectedHead was supplied and matched
	// the final receipt exactly (VER-11). False does not mean the chain is
	// tampered — it means completeness was never claimed.
	Complete bool
}

// VerifyChain checks receipts — already ordered by ascending Seq — against
// trustedKeys. A returned error means the input or configuration itself
// could not be checked at all (VER-09: empty sources, unknown key IDs,
// unsupported versions) — the caller should treat that as an I/O/config
// failure, not a tamper finding. A returned VerifyResult with OK == false
// means verification ran and found a mismatch (VER-05 through VER-08).
//
// Genesis anchoring (the first receipt must be Seq 1 with a zero
// PrevHash) is always required: this milestone verifies whole local
// ledgers or JSONL dumps of one. Bounded, manifest-anchored exports that
// can start mid-chain are a later phase's scope.
//
// If expectedHead is non-nil, VerifyChain additionally requires the final
// receipt to match it exactly before claiming completeness (VER-11).
func VerifyChain(receipts []Receipt, trustedKeys []TrustedKey, expectedHead *ExpectedHead) (VerifyResult, error) {
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
	var prevHash [32]byte
	for i := range receipts {
		r := receipts[i]

		if r.Seq != uint64(i+1) {
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
		if !signer.Verify(key.PublicKey, entryHash[:], r.Signature) {
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
