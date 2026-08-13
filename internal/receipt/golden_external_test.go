/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/receipt"
)

type fixtureManifest struct {
	Version      int    `json:"version"`
	Domain       string `json:"domain"`
	BinaryFile   string `json:"binary_file"`
	BinaryLength int    `json:"binary_length"`
	BinarySHA256 string `json:"binary_sha256"`
	Receipt      struct {
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
		EntryHashNoise  string   `json:"entry_hash_noise"`
		SignerKID       string   `json:"signer_kid"`
		SignatureNoise  string   `json:"signature_noise"`
	} `json:"receipt"`
}

func TestGoldenFixtures(t *testing.T) {
	manifestBytes, err := os.ReadFile("testdata/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest fixtureManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 {
		t.Fatalf("version = %d, want 1", manifest.Version)
	}
	if manifest.Domain != receipt.HashDomainV1 {
		t.Fatalf("domain = %q, want %q", manifest.Domain, receipt.HashDomainV1)
	}
	if manifest.Receipt.EntryHashNoise == "" || manifest.Receipt.SignatureNoise == "" {
		t.Fatal("derived-field noise must be documented")
	}
	binaryBytes, err := os.ReadFile("testdata/v1/" + manifest.BinaryFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(binaryBytes) != manifest.BinaryLength {
		t.Fatalf("binary length = %d, want %d", len(binaryBytes), manifest.BinaryLength)
	}
	digest := sha256.Sum256(binaryBytes)
	if hex.EncodeToString(digest[:]) != manifest.BinarySHA256 {
		t.Fatal("binary SHA-256 mismatch")
	}

	value := receiptFromManifest(t, manifest)
	production, err := receipt.CanonicalHashInput(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(production, binaryBytes) || !bytes.Equal(referenceEncode(value), binaryBytes) {
		t.Fatal("fixture differs from production or reference encoder")
	}
}

func receiptFromManifest(t *testing.T, manifest fixtureManifest) receipt.Receipt {
	t.Helper()
	sequence, err := strconv.ParseUint(manifest.Receipt.Seq, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	timestamp, err := strconv.ParseUint(manifest.Receipt.TimestampUnixNS, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	latency, err := strconv.ParseInt(manifest.Receipt.LatencyMS, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	paramsBytes, err := hex.DecodeString(manifest.Receipt.ParamsSHA256)
	if err != nil || len(paramsBytes) != 32 {
		t.Fatal("invalid params hash")
	}
	prevBytes, err := hex.DecodeString(manifest.Receipt.PrevHash)
	if err != nil || len(prevBytes) != 32 {
		t.Fatal("invalid previous hash")
	}
	value := receipt.Receipt{
		Seq: sequence, TimestampUnixNS: timestamp, HumanPrincipal: manifest.Receipt.HumanPrincipal,
		AgentKeyID: manifest.Receipt.AgentKeyID, DelegationChain: manifest.Receipt.DelegationChain,
		Service: manifest.Receipt.Service, Action: manifest.Receipt.Action,
		PolicyDecision: manifest.Receipt.PolicyDecision, StatusCode: manifest.Receipt.StatusCode,
		LatencyMS: latency, Error: manifest.Receipt.Error, SignerKID: manifest.Receipt.SignerKID,
	}
	copy(value.ParamsSHA256[:], paramsBytes)
	copy(value.PrevHash[:], prevBytes)
	return value
}
