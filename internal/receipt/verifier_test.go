/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/signer"
)

func appendN(t *testing.T, ledger *Ledger, n int) []Receipt {
	t.Helper()
	out := make([]Receipt, 0, n)
	for i := 0; i < n; i++ {
		r, err := ledger.Append(t.Context(), testDraft("github", "list_repos"))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		out = append(out, r)
	}
	return out
}

func trustedKeysFrom(t *testing.T, store *signer.Store) []TrustedKey {
	t.Helper()
	keys, err := store.PublicKeys()
	if err != nil {
		t.Fatalf("PublicKeys: %v", err)
	}
	out := make([]TrustedKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, TrustedKey{
			KID:           k.KID,
			PublicKey:     k.PublicKey,
			ValidFromSeq:  k.ValidFromSeq,
			ValidUntilSeq: k.ValidUntilSeq,
		})
	}
	return out
}

func newTestLedgerAndStore(t *testing.T) (*Ledger, *signer.Store) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "agentgate.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("db.RunMigrations: %v", err)
	}
	store, err := signer.NewStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("signer.NewStore: %v", err)
	}
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}
	return NewLedger(database, store), store
}

func TestVerifyChain_ValidChainPasses(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 5)
	trusted := trustedKeysFrom(t, store)

	result, err := VerifyChain(receipts, trusted, Anchor{}, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.OK {
		t.Fatalf("OK = false, reason = %s at seq %d", result.Reason, result.FailedAtSeq)
	}
	if result.VerifiedCount != 5 {
		t.Fatalf("VerifiedCount = %d, want 5", result.VerifiedCount)
	}
	if result.Complete {
		t.Fatal("Complete = true without an ExpectedHead")
	}
}

// TestVerifyChain_ModifiedReceiptFails covers VER-05.
func TestVerifyChain_ModifiedReceiptFails(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 5)
	trusted := trustedKeysFrom(t, store)

	receipts[2].StatusCode = 404 // modify a field after the fact (still a valid field value); entry_hash no longer matches

	result, err := VerifyChain(receipts, trusted, Anchor{}, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.OK {
		t.Fatal("OK = true for a modified receipt")
	}
	if result.Reason != ReasonEntryHashMismatch {
		t.Fatalf("Reason = %s, want %s", result.Reason, ReasonEntryHashMismatch)
	}
	if result.FailedAtSeq != 3 {
		t.Fatalf("FailedAtSeq = %d, want 3", result.FailedAtSeq)
	}
}

// TestVerifyChain_InteriorDeletedReceiptFails covers VER-06.
func TestVerifyChain_InteriorDeletedReceiptFails(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 5)
	trusted := trustedKeysFrom(t, store)

	// Delete the interior row at seq 3.
	tampered := append(append([]Receipt{}, receipts[:2]...), receipts[3:]...)

	result, err := VerifyChain(tampered, trusted, Anchor{}, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.OK {
		t.Fatal("OK = true for a chain missing an interior receipt")
	}
	if result.Reason != ReasonSequenceGap {
		t.Fatalf("Reason = %s, want %s", result.Reason, ReasonSequenceGap)
	}
}

// TestVerifyChain_InsertedReceiptFails covers VER-07.
func TestVerifyChain_InsertedReceiptFails(t *testing.T) {
	t.Parallel()
	ledgerA, storeA := newTestLedgerAndStore(t)
	chainA := appendN(t, ledgerA, 3)
	trusted := trustedKeysFrom(t, storeA)

	// A foreign receipt from an entirely different chain, spliced in.
	ledgerB, _ := newTestLedgerAndStore(t)
	foreign := appendN(t, ledgerB, 1)[0]
	foreign.Seq = 4 // claim the next sequence in chain A

	tampered := append(append([]Receipt{}, chainA...), foreign)

	result, err := VerifyChain(tampered, trusted, Anchor{}, nil)
	if err == nil && result.OK {
		t.Fatal("verification passed for a spliced-in foreign receipt")
	}
	// The foreign receipt's prev_hash cannot match chain A's real head, or
	// (if it happens to share a signer_kid) its entry hash and signature
	// were computed for a different chain and will not verify either way.
	if err == nil && result.Reason != ReasonPrevHashMismatch && result.Reason != ReasonEntryHashMismatch && result.Reason != ReasonSignatureInvalid {
		t.Fatalf("Reason = %s, want prev_hash_mismatch, entry_hash_mismatch, or signature_invalid", result.Reason)
	}
}

// TestVerifyChain_ForgedSignatureFails covers VER-08.
func TestVerifyChain_ForgedSignatureFails(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 3)
	trusted := trustedKeysFrom(t, store)

	receipts[1].Signature[0] ^= 0xFF // flip a byte; hash still matches, signature does not

	result, err := VerifyChain(receipts, trusted, Anchor{}, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.OK {
		t.Fatal("OK = true for a forged signature")
	}
	if result.Reason != ReasonSignatureInvalid {
		t.Fatalf("Reason = %s, want %s", result.Reason, ReasonSignatureInvalid)
	}
	if result.FailedAtSeq != 2 {
		t.Fatalf("FailedAtSeq = %d, want 2", result.FailedAtSeq)
	}
}

// TestVerifyChain_EmptyInputIsConfigError covers half of VER-09.
func TestVerifyChain_EmptyInputIsConfigError(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	trusted := trustedKeysFrom(t, store)

	if _, err := VerifyChain(nil, trusted, Anchor{}, nil); err != ErrEmptyChain {
		t.Fatalf("err = %v, want ErrEmptyChain", err)
	}
}

// TestVerifyChain_NoTrustedKeysIsConfigError covers the other half of
// VER-09: a trust file with zero keys.
func TestVerifyChain_NoTrustedKeysIsConfigError(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 1)

	if _, err := VerifyChain(receipts, nil, Anchor{}, nil); err != ErrNoTrustedKeys {
		t.Fatalf("err = %v, want ErrNoTrustedKeys", err)
	}
}

// TestVerifyChain_UnknownSignerKIDIsConfigError covers VER-09.
func TestVerifyChain_UnknownSignerKIDIsConfigError(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 1)

	// A trust file that simply does not include the key that actually
	// signed this receipt (a stale or wrong trust file).
	otherLedger, otherStore := newTestLedgerAndStore(t)
	_ = appendN(t, otherLedger, 1)
	wrongTrust := trustedKeysFrom(t, otherStore)

	_, err := VerifyChain(receipts, wrongTrust, Anchor{}, nil)
	if err == nil {
		t.Fatal("err = nil, want ErrUnknownSignerKID")
	}
}

// TestVerifyChain_RotatedKeyBindsToItsValidityInterval covers VER-03 under
// this milestone's trust model: a receipt signed outside its own signer's
// validity interval fails, even with a technically correct signature.
func TestVerifyChain_RotatedKeyBindsToItsValidityInterval(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	before := appendN(t, ledger, 2)

	if _, _, err := store.Rotate(3); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	after := appendN(t, ledger, 2)
	all := append(append([]Receipt{}, before...), after...)
	trusted := trustedKeysFrom(t, store)

	result, err := VerifyChain(all, trusted, Anchor{}, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.OK {
		t.Fatalf("OK = false across a rotation, reason = %s at seq %d", result.Reason, result.FailedAtSeq)
	}

	// Now claim the retired key signed something at a seq beyond its
	// retirement — this must fail even though the signature itself is
	// mathematically valid under that key.
	forged := all[3] // signed by the new key at seq 4
	forged.SignerKID = all[0].SignerKID
	tampered := append([]Receipt{}, all...)
	tampered[3] = forged
	// Re-point prev_hash/seq stay the same; only signer_kid is wrong, so
	// this must fail on the interval check before signature verification
	// even gets a chance to also fail.
	result2, err := VerifyChain(tampered, trusted, Anchor{}, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result2.OK {
		t.Fatal("OK = true for a receipt claiming a retired key")
	}
}

// TestVerifyChain_ExpectedHeadProvesCompleteness covers VER-11.
func TestVerifyChain_ExpectedHeadProvesCompleteness(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 4)
	trusted := trustedKeysFrom(t, store)
	last := receipts[len(receipts)-1]

	result, err := VerifyChain(receipts, trusted, Anchor{}, &ExpectedHead{Seq: last.Seq, EntryHash: last.EntryHash})
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.OK || !result.Complete {
		t.Fatalf("OK = %v, Complete = %v, want both true", result.OK, result.Complete)
	}
}

// TestVerifyChain_ExpectedHeadMismatchDetectsTruncation covers VER-11's
// other half: a chain that is internally consistent but shorter than the
// independently trusted head must fail, not silently pass as "complete."
func TestVerifyChain_ExpectedHeadMismatchDetectsTruncation(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 4)
	trusted := trustedKeysFrom(t, store)

	truncated := receipts[:2] // internally valid, but the tail is missing

	result, err := VerifyChain(truncated, trusted, Anchor{}, &ExpectedHead{Seq: 4, EntryHash: receipts[3].EntryHash})
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.OK {
		t.Fatal("OK = true for a truncated chain against a mismatched expected head")
	}
	if result.Reason != ReasonExpectedHeadMismatch {
		t.Fatalf("Reason = %s, want %s", result.Reason, ReasonExpectedHeadMismatch)
	}
}

func TestParseExpectedHead(t *testing.T) {
	t.Parallel()
	var hash [32]byte
	hash[0], hash[1], hash[2] = 1, 2, 3
	s := "42:" + hex.EncodeToString(hash[:])
	got, err := ParseExpectedHead(s)
	if err != nil {
		t.Fatalf("ParseExpectedHead: %v", err)
	}
	if got.Seq != 42 || got.EntryHash != hash {
		t.Fatalf("got %+v", got)
	}

	if _, err := ParseExpectedHead("not-valid"); err == nil {
		t.Fatal("expected an error for a malformed --expected-head value")
	}
}

// TestVerifyChain_NonGenesisAnchorVerifiesMidChainSlice covers the Anchor
// API added for bounded/partial exports (Phase 7): a slice that starts
// mid-chain must verify successfully when given the real predecessor's
// (seq, entry_hash) as its Anchor, proving VerifyChain no longer requires
// every verified chain to start at seq=1.
func TestVerifyChain_NonGenesisAnchorVerifiesMidChainSlice(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 6)
	trusted := trustedKeysFrom(t, store)

	// seq 1..2 stand in for the anchor; seq 3..5 is the slice under test.
	anchor := Anchor{Seq: 2, EntryHash: receipts[1].EntryHash}
	slice := receipts[2:5]

	result, err := VerifyChain(slice, trusted, anchor, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.OK {
		t.Fatalf("OK = false, reason = %s at seq %d", result.Reason, result.FailedAtSeq)
	}
	if result.VerifiedCount != 3 {
		t.Fatalf("VerifiedCount = %d, want 3", result.VerifiedCount)
	}
}

// TestVerifyChain_WrongAnchorHashDetected proves a forged or mismatched
// anchor is rejected rather than silently accepted: an attacker cannot
// splice a mid-chain slice onto a fabricated predecessor.
func TestVerifyChain_WrongAnchorHashDetected(t *testing.T) {
	t.Parallel()
	ledger, store := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 6)
	trusted := trustedKeysFrom(t, store)

	wrongAnchor := Anchor{Seq: 2, EntryHash: receipts[0].EntryHash} // real seq=1 hash, not seq=2's
	slice := receipts[2:5]

	result, err := VerifyChain(slice, trusted, wrongAnchor, nil)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.OK {
		t.Fatal("OK = true with a forged anchor hash")
	}
	if result.Reason != ReasonPrevHashMismatch {
		t.Fatalf("Reason = %s, want %s", result.Reason, ReasonPrevHashMismatch)
	}
}
