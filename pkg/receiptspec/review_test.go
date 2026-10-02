/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestChainNextDetachesDelegationChain(t *testing.T) {
	signer, pub := testSigner(t)
	chain := receiptspec.NewChain(signer, 0, [32]byte{})
	f := fields(1)
	f.DelegationChain = []string{"a", "b"}
	r, err := chain.Next(f)
	if err != nil {
		t.Fatal(err)
	}
	f.DelegationChain[0] = "mutated"
	if err := receiptspec.VerifyReceipt(r, pub); err != nil {
		t.Fatalf("receipt changed after caller mutated its slice: %v", err)
	}
}

func TestSealDetachesDelegationChain(t *testing.T) {
	signer, pub := testSigner(t)
	in := fields(1)
	in.DelegationChain = []string{"a"}
	chain := receiptspec.NewChain(signer, 0, [32]byte{})
	base, err := chain.Next(in)
	if err != nil {
		t.Fatal(err)
	}
	base.DelegationChain = []string{"x", "y"}
	sealed, err := receiptspec.Seal(base, signer)
	if err != nil {
		t.Fatal(err)
	}
	base.DelegationChain[0] = "mutated"
	if err := receiptspec.VerifyReceipt(sealed, pub); err != nil {
		t.Fatalf("sealed receipt aliases input slice: %v", err)
	}
}

func TestVerifyChainRejectsMalformedPublicKey(t *testing.T) {
	receipts, signer, _ := buildChain(t, 2)
	for _, key := range [][]byte{nil, {1, 2, 3}} {
		trusted := []receiptspec.TrustedKey{{KID: signer.KID(), PublicKey: key}}
		res, err := receiptspec.VerifyChain(receipts, trusted, receiptspec.Anchor{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.OK {
			t.Fatalf("key %v: chain verified with a malformed public key", key)
		}
	}
}

func TestVerifyManifestRejectsMalformedPublicKey(t *testing.T) {
	m := receiptspec.ExportManifest{SignerKID: "ed25519:bad"}
	for _, key := range [][]byte{nil, {1, 2, 3}} {
		trusted := []receiptspec.TrustedKey{{KID: "ed25519:bad", PublicKey: key}}
		err := receiptspec.VerifyManifest(m, trusted, nil)
		if !errors.Is(err, receiptspec.ErrManifestSignatureInvalid) {
			t.Fatalf("key %v: got %v, want ErrManifestSignatureInvalid", key, err)
		}
	}
}

func TestCanonicalRecordRejectsOversizedInput(t *testing.T) {
	big := `{"a":"` + strings.Repeat("x", 1<<20) + `"}`
	if _, err := receiptspec.CanonicalRecord([]byte(big)); !errors.Is(err, receiptspec.ErrInvalidRecord) {
		t.Fatalf("got %v, want ErrInvalidRecord", err)
	}
}
