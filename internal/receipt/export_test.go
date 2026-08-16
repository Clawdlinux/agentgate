/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"errors"
	"testing"
)

func TestComputeKeysetDigest_OrderIndependent(t *testing.T) {
	t.Parallel()
	a := TrustedKey{KID: "a", PublicKey: []byte{1, 2, 3}, ValidFromSeq: 1}
	b := TrustedKey{KID: "b", PublicKey: []byte{4, 5, 6}, ValidFromSeq: 5}

	d1 := ComputeKeysetDigest([]TrustedKey{a, b})
	d2 := ComputeKeysetDigest([]TrustedKey{b, a})
	if d1 != d2 {
		t.Fatal("keyset digest depends on input order")
	}
}

func TestComputeKeysetDigest_DiffersOnChange(t *testing.T) {
	t.Parallel()
	a := TrustedKey{KID: "a", PublicKey: []byte{1, 2, 3}, ValidFromSeq: 1}
	aChanged := TrustedKey{KID: "a", PublicKey: []byte{1, 2, 3}, ValidFromSeq: 2}

	d1 := ComputeKeysetDigest([]TrustedKey{a})
	d2 := ComputeKeysetDigest([]TrustedKey{aChanged})
	if d1 == d2 {
		t.Fatal("digest did not change when a key's validity interval changed")
	}
}

func testManifest(keys []TrustedKey) ExportManifest {
	return ExportManifest{
		FormatVersion:  ExportFormatVersion,
		RequestedFrom:  1,
		ResolvedTo:     3,
		Count:          3,
		AnchorSeq:      0,
		FirstEntryHash: [32]byte{1},
		LastEntryHash:  [32]byte{2},
		HeadSeq:        3,
		HeadHash:       [32]byte{2},
		KeysetDigest:   ComputeKeysetDigest(keys),
	}
}

func TestManifest_SignAndVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	keys := trustedKeysFrom(t, store)
	active, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}

	m := SignManifest(testManifest(keys), active.KID, priv)
	if err := VerifyManifest(m, keys, keys); err != nil {
		t.Fatalf("VerifyManifest: %v", err)
	}
}

func TestManifest_ForgedSignatureFails(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	keys := trustedKeysFrom(t, store)
	active, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}

	m := SignManifest(testManifest(keys), active.KID, priv)
	m.Signature[0] ^= 0xFF

	if err := VerifyManifest(m, keys, keys); err != ErrManifestSignatureInvalid {
		t.Fatalf("err = %v, want ErrManifestSignatureInvalid", err)
	}
}

func TestManifest_KeysetDigestMismatchFails(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	keys := trustedKeysFrom(t, store)
	active, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}

	m := SignManifest(testManifest(keys), active.KID, priv)
	tamperedKeys := append([]TrustedKey{}, keys...)
	tamperedKeys = append(tamperedKeys, TrustedKey{KID: "extra", PublicKey: make([]byte, 32), ValidFromSeq: 99})

	if err := VerifyManifest(m, keys, tamperedKeys); err != ErrKeysetDigestMismatch {
		t.Fatalf("err = %v, want ErrKeysetDigestMismatch", err)
	}
}

func TestManifest_UnknownSignerKIDFails(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	keys := trustedKeysFrom(t, store)
	active, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}
	m := SignManifest(testManifest(keys), active.KID, priv)

	if err := VerifyManifest(m, nil, keys); !errors.Is(err, ErrUnknownSignerKID) {
		t.Fatalf("err = %v, want ErrUnknownSignerKID", err)
	}
}

func TestManifestLine_MarshalParseRoundTrip(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	keys := trustedKeysFrom(t, store)
	active, priv, err := store.LoadOrCreateActive(1)
	if err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}
	want := SignManifest(testManifest(keys), active.KID, priv)

	line, err := MarshalManifestLine(want)
	if err != nil {
		t.Fatalf("MarshalManifestLine: %v", err)
	}
	if DetectJSONLLineType(line) != "manifest" {
		t.Fatalf("DetectJSONLLineType = %s, want manifest", DetectJSONLLineType(line))
	}
	got, err := ParseManifestLine(line)
	if err != nil {
		t.Fatalf("ParseManifestLine: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestKeyLine_MarshalParseRoundTrip(t *testing.T) {
	t.Parallel()
	_, store := newTestLedgerAndStore(t)
	keys := trustedKeysFrom(t, store)
	if len(keys) == 0 {
		t.Fatal("expected at least one key")
	}
	want := keys[0]

	line, err := MarshalKeyLine(want)
	if err != nil {
		t.Fatalf("MarshalKeyLine: %v", err)
	}
	if DetectJSONLLineType(line) != "key" {
		t.Fatalf("DetectJSONLLineType = %s, want key", DetectJSONLLineType(line))
	}
	got, err := ParseKeyLine(line)
	if err != nil {
		t.Fatalf("ParseKeyLine: %v", err)
	}
	if got.KID != want.KID || string(got.PublicKey) != string(want.PublicKey) || got.ValidFromSeq != want.ValidFromSeq {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestDetectJSONLLineType_DefaultsToReceiptWhenTypeAbsent(t *testing.T) {
	t.Parallel()
	// Phase 5's original plain format: no "type" field at all.
	line := []byte(`{"format_version":1,"seq":1}`)
	if got := DetectJSONLLineType(line); got != "receipt" {
		t.Fatalf("DetectJSONLLineType = %s, want receipt", got)
	}
}
