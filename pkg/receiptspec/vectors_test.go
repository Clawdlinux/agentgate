/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type vectorOutcome struct {
	Class         string `json:"class"`
	VerifiedCount int    `json:"verified_count"`
	FailedAtSeq   uint64 `json:"failed_at_seq"`
	Complete      bool   `json:"complete"`
}

type goldenVector struct {
	Name     string        `json:"name"`
	JSONL    string        `json:"jsonl"`
	Keys     string        `json:"keys"`
	Expected vectorOutcome `json:"expected"`
}

func loadVectors(t *testing.T) []goldenVector {
	t.Helper()
	var catalog struct {
		FormatVersion int            `json:"format_version"`
		Vectors       []goldenVector `json:"vectors"`
	}
	readVectorJSON(t, "vectors.json", &catalog)
	if catalog.FormatVersion != 1 || len(catalog.Vectors) == 0 {
		t.Fatal("expected a nonempty v1 vector catalog")
	}
	files, err := filepath.Glob("testdata/v1/*.jsonl")
	if err != nil || len(files) != len(catalog.Vectors) {
		t.Fatalf("every JSONL fixture must be listed: files=%d vectors=%d err=%v", len(files), len(catalog.Vectors), err)
	}
	seen := make(map[string]bool)
	for _, v := range catalog.Vectors {
		if seen[v.JSONL] {
			t.Fatalf("duplicate vector file %q", v.JSONL)
		}
		seen[v.JSONL] = true
	}
	return catalog.Vectors
}

func readVectorJSON(t *testing.T, name string, out any) {
	t.Helper()
	if err := json.Unmarshal(readVectorFile(t, name), out); err != nil {
		t.Fatal(err)
	}
}

func readVectorFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata/v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestV1GoldenVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			bundle, err := ReadJSONL(strings.NewReader(string(readVectorFile(t, v.JSONL))))
			if err != nil {
				t.Fatal(err)
			}
			trust, err := LoadTrustedKeys(readVectorFile(t, v.Keys))
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range []struct {
				name string
				keys []TrustedKey
			}{{"external-trust", trust}, {"embedded-trust", nil}} {
				t.Run(source.name, func(t *testing.T) {
					result, err := VerifyBundle(bundle, source.keys, nil)
					got := vectorOutcome{
						Class: vectorResultClass(t, result, err), VerifiedCount: result.VerifiedCount,
						FailedAtSeq: result.FailedAtSeq, Complete: result.Complete,
					}
					if got != v.Expected {
						t.Fatalf("got %+v (error %v), want %+v", got, err, v.Expected)
					}
				})
			}
		})
	}
}

func TestV1WorkedExample(t *testing.T) {
	var example struct {
		SeedHex       string `json:"seed_hex"`
		PublicKeyHex  string `json:"public_key_hex"`
		KID           string `json:"kid"`
		PreimageHex   string `json:"preimage_hex"`
		PreimageBytes int    `json:"preimage_bytes"`
		EntryHash     string `json:"entry_hash"`
		Signature     string `json:"signature"`
	}
	readVectorJSON(t, "example.json", &example)
	seed, err := hex.DecodeString(example.SeedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("invalid example seed: %v", err)
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	if hex.EncodeToString(public) != example.PublicKeyHex || ComputeKID(public) != example.KID {
		t.Fatal("worked example key derivation differs")
	}
	bundle, err := ReadJSONL(strings.NewReader(string(readVectorFile(t, "valid-genesis.jsonl"))))
	if err != nil || len(bundle.Receipts) != 1 {
		t.Fatalf("invalid worked example: %v", err)
	}
	r := bundle.Receipts[0]
	preimage, err := CanonicalHashInput(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(preimage) != example.PreimageBytes || hex.EncodeToString(preimage) != example.PreimageHex {
		t.Fatal("worked example preimage differs")
	}
	hash := sha256.Sum256(preimage)
	if hex.EncodeToString(hash[:]) != example.EntryHash || r.EntryHash != hash {
		t.Fatal("worked example entry hash differs")
	}
	signature := ed25519.Sign(private, hash[:])
	if hex.EncodeToString(signature) != example.Signature || hex.EncodeToString(r.Signature[:]) != example.Signature {
		t.Fatal("worked example signature differs")
	}
	if err := VerifyReceipt(r, public); err != nil {
		t.Fatal(err)
	}
}

func vectorResultClass(t *testing.T, result VerifyResult, err error) string {
	t.Helper()
	if err == nil {
		if result.OK {
			return "ok"
		}
		if result.Reason == "" {
			t.Fatal("failed verification without a reason")
		}
		return result.Reason
	}
	prefix := ""
	var manifestError *ManifestError
	if errors.As(err, &manifestError) {
		prefix = "ManifestError/"
	}
	for _, candidate := range []struct {
		err   error
		class string
	}{
		{ErrUnknownSignerKID, "ErrUnknownSignerKID"},
		{ErrEmptyChain, "ErrEmptyChain"},
		{ErrNoTrustedKeys, "ErrNoTrustedKeys"},
		{ErrManifestSignatureInvalid, "ErrManifestSignatureInvalid"},
		{ErrKeysetDigestMismatch, "ErrKeysetDigestMismatch"},
	} {
		if errors.Is(err, candidate.err) {
			return prefix + candidate.class
		}
	}
	t.Fatalf("unclassified verifier error: %v", err)
	return ""
}

type encodingLock struct {
	Receipts []string `json:"receipts"`
	Manifest string   `json:"manifest,omitempty"`
}

func TestV1VersionLock(t *testing.T) {
	const guidance = "v1 encoding changed: bump format_version and write docs/spec/RECEIPT-v2.md; preserve the v1 lock"
	if JSONLFormatVersion != 1 || ExportFormatVersion != 1 ||
		HashDomainV1 != "agentgate.receipt.hash.v1" ||
		ExportManifestDomainV1 != "agentgate.receipt.export-manifest.v1" {
		t.Fatal(guidance)
	}
	// This file is deliberately outside the generator's outputs.
	var locks map[string]encodingLock
	readVectorJSON(t, "encoding-lock.json", &locks)
	checked := 0
	for _, v := range loadVectors(t) {
		if v.Expected.Class != "ok" {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			want, ok := locks[v.Name]
			if !ok {
				t.Fatalf("missing committed lock for %s", v.Name)
			}
			bundle, err := ReadJSONL(strings.NewReader(string(readVectorFile(t, v.JSONL))))
			if err != nil {
				t.Fatal(err)
			}
			var got encodingLock
			for _, r := range bundle.Receipts {
				preimage, err := CanonicalHashInput(r)
				if err != nil {
					t.Fatalf("%s: %v", guidance, err)
				}
				hash := sha256.Sum256(preimage)
				got.Receipts = append(got.Receipts, hex.EncodeToString(hash[:]))
			}
			if bundle.Manifest != nil {
				hash := sha256.Sum256(canonicalManifestInput(*bundle.Manifest))
				got.Manifest = hex.EncodeToString(hash[:])
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s\ngot: %+v\nwant: %+v", guidance, got, want)
			}
		})
		checked++
	}
	if checked != len(locks) {
		t.Fatalf("lock coverage differs: checked=%d committed=%d", checked, len(locks))
	}
}
