/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func testSigner(t *testing.T) (receiptspec.Signer, ed25519.PublicKey) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	return receiptspec.NewEd25519Signer(priv), priv.Public().(ed25519.PublicKey)
}

func fields(i int) receiptspec.Fields {
	return receiptspec.Fields{
		TimestampUnixNS: uint64(1_700_000_000_000_000_000 + i),
		HumanPrincipal:  "alice@example.com",
		AgentKeyID:      "agent-1",
		Service:         "operator",
		Action:          "decision:scale",
		PolicyDecision:  receiptspec.DecisionAllow,
		StatusCode:      200,
		LatencyMS:       int64(i),
	}
}

func buildChain(t *testing.T, n int) ([]receiptspec.Receipt, receiptspec.Signer, ed25519.PublicKey) {
	t.Helper()
	s, pub := testSigner(t)
	c := receiptspec.NewChain(s, 0, [32]byte{})
	out := make([]receiptspec.Receipt, 0, n)
	for i := 0; i < n; i++ {
		r, err := c.Next(fields(i))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out, s, pub
}

func TestChainMatchesGoldenVector(t *testing.T) {
	var m struct {
		BinaryFile string `json:"binary_file"`
		Receipt    struct {
			Seq             string   `json:"seq"`
			TimestampUnixNS string   `json:"timestamp_unix_ns"`
			HumanPrincipal  string   `json:"human_principal"`
			AgentKeyID      string   `json:"agent_key_id"`
			DelegationChain []string `json:"delegation_chain"`
			Service         string   `json:"service"`
			Action          string   `json:"action"`
			ParamsSHA256    string   `json:"params_sha256"`
			PolicyDecision  string   `json:"policy_decision"`
			StatusCode      int      `json:"status_code"`
			LatencyMS       string   `json:"latency_ms"`
			Error           string   `json:"error"`
			PrevHash        string   `json:"prev_hash"`
			SignerKID       string   `json:"signer_kid"`
		} `json:"receipt"`
	}
	raw, err := os.ReadFile("../../internal/receipt/testdata/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("../../internal/receipt/testdata/v1/" + m.BinaryFile)
	if err != nil {
		t.Fatal(err)
	}
	seq, _ := strconv.ParseUint(m.Receipt.Seq, 10, 64)
	ts, _ := strconv.ParseUint(m.Receipt.TimestampUnixNS, 10, 64)
	latency, _ := strconv.ParseInt(m.Receipt.LatencyMS, 10, 64)
	var prev, params [32]byte
	mustHex(t, m.Receipt.PrevHash, prev[:])
	mustHex(t, m.Receipt.ParamsSHA256, params[:])

	_, pub := testSigner(t)
	s := kidSigner{kid: m.Receipt.SignerKID, priv: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))}
	c := receiptspec.NewChain(s, seq-1, prev)
	r, err := c.Next(receiptspec.Fields{
		TimestampUnixNS: ts, HumanPrincipal: m.Receipt.HumanPrincipal, AgentKeyID: m.Receipt.AgentKeyID,
		DelegationChain: m.Receipt.DelegationChain, Service: m.Receipt.Service, Action: m.Receipt.Action,
		ParamsSHA256: params, PolicyDecision: m.Receipt.PolicyDecision, StatusCode: m.Receipt.StatusCode,
		LatencyMS: latency, Error: m.Receipt.Error,
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := receiptspec.CanonicalHashInput(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, golden) {
		t.Fatal("Chain receipt preimage differs from the golden vector")
	}
	if r.EntryHash != sha256.Sum256(golden) {
		t.Fatal("entry hash differs from SHA-256 of the golden vector")
	}
	if err := receiptspec.VerifyReceipt(r, pub); err != nil {
		t.Fatal(err)
	}
}

type kidSigner struct {
	kid  string
	priv ed25519.PrivateKey
}

func (s kidSigner) KID() string                     { return s.kid }
func (s kidSigner) Sign(msg []byte) ([]byte, error) { return ed25519.Sign(s.priv, msg), nil }

func mustHex(t *testing.T, s string, dst []byte) {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(dst) {
		t.Fatalf("bad hex %q", s)
	}
	copy(dst, b)
}

func TestChainJSONLRoundTrip(t *testing.T) {
	receipts, s, pub := buildChain(t, 5)

	var buf bytes.Buffer
	if err := receiptspec.WriteJSONL(&buf, receipts); err != nil {
		t.Fatal(err)
	}
	trustJSON, _ := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kid": s.KID(), "public_key_hex": hex.EncodeToString(pub), "valid_from_seq": 1, "valid_until_seq": nil,
	}}})
	trusted, err := receiptspec.LoadTrustedKeys(trustJSON)
	if err != nil {
		t.Fatal(err)
	}

	bundle, err := receiptspec.ReadJSONL(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	head := receiptspec.ExpectedHead{Seq: 5, EntryHash: receipts[4].EntryHash}
	result, err := receiptspec.VerifyBundle(bundle, trusted, &head)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Complete || result.VerifiedCount != 5 {
		t.Fatalf("result = %+v", result)
	}

	bundle.Receipts[2].LatencyMS++
	result, err = receiptspec.VerifyBundle(bundle, trusted, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.FailedAtSeq != 3 || result.Reason != receiptspec.ReasonEntryHashMismatch {
		t.Fatalf("tampered result = %+v", result)
	}
}

func TestChainHeadAdvancesOnlyOnSuccess(t *testing.T) {
	s, _ := testSigner(t)
	c := receiptspec.NewChain(s, 0, [32]byte{})
	first, err := c.Next(fields(0))
	if err != nil {
		t.Fatal(err)
	}
	bad := fields(1)
	bad.PolicyDecision = "maybe"
	if _, err := c.Next(bad); !errors.Is(err, receiptspec.ErrInvalidField) {
		t.Fatalf("err = %v, want ErrInvalidField", err)
	}
	if seq, hash := c.Head(); seq != 1 || hash != first.EntryHash {
		t.Fatalf("head moved after failed Next: %d", seq)
	}
	second, err := c.Next(fields(1))
	if err != nil || second.Seq != 2 || second.PrevHash != first.EntryHash {
		t.Fatalf("second = %+v, err = %v", second, err)
	}
}

type shortSigner struct{}

func (shortSigner) KID() string                 { return "ed25519:short" }
func (shortSigner) Sign([]byte) ([]byte, error) { return []byte{1, 2, 3}, nil }

func TestSealRejectsMalformedSignature(t *testing.T) {
	c := receiptspec.NewChain(shortSigner{}, 0, [32]byte{})
	if _, err := c.Next(fields(0)); !errors.Is(err, receiptspec.ErrSignature) {
		t.Fatalf("err = %v, want ErrSignature", err)
	}
}

func TestVerifyReceipt(t *testing.T) {
	receipts, _, pub := buildChain(t, 1)
	r := receipts[0]
	if err := receiptspec.VerifyReceipt(r, pub); err != nil {
		t.Fatal(err)
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	if err := receiptspec.VerifyReceipt(r, other); !errors.Is(err, receiptspec.ErrSignature) {
		t.Fatalf("wrong key err = %v", err)
	}
	r.Service = "other"
	if err := receiptspec.VerifyReceipt(r, pub); !errors.Is(err, receiptspec.ErrEntryHashMismatch) {
		t.Fatalf("tampered err = %v", err)
	}
}

func TestComputeKIDMatchesSigner(t *testing.T) {
	s, pub := testSigner(t)
	if s.KID() != receiptspec.ComputeKID(pub) {
		t.Fatal("NewEd25519Signer KID differs from ComputeKID")
	}
}

func TestVerifyBundleManifestError(t *testing.T) {
	receipts, s, pub := buildChain(t, 2)
	key := receiptspec.TrustedKey{KID: s.KID(), PublicKey: pub, ValidFromSeq: 1}
	m := receiptspec.ExportManifest{FormatVersion: 1, ResolvedTo: 2, Count: 2, LastEntryHash: receipts[1].EntryHash,
		KeysetDigest: receiptspec.ComputeKeysetDigest([]receiptspec.TrustedKey{key})}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	m = receiptspec.SignManifest(m, s.KID(), other)

	_, err := receiptspec.VerifyBundle(receiptspec.Bundle{Receipts: receipts, Keys: []receiptspec.TrustedKey{key}, Manifest: &m}, nil, nil)
	var me *receiptspec.ManifestError
	if !errors.As(err, &me) || !errors.Is(err, receiptspec.ErrManifestSignatureInvalid) {
		t.Fatalf("err = %v, want ManifestError wrapping ErrManifestSignatureInvalid", err)
	}
}
