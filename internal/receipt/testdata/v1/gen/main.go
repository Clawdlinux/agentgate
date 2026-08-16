/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const domain = "agentgate.receipt.hash.v1"

type fixtureReceipt struct {
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
}

type manifest struct {
	Version       int               `json:"version"`
	Domain        string            `json:"domain"`
	Receipt       fixtureReceipt    `json:"receipt"`
	BinaryFile    string            `json:"binary_file"`
	BinaryLength  int               `json:"binary_length"`
	BinarySHA256  string            `json:"binary_sha256"`
	InvalidBounds map[string]string `json:"invalid_bounds"`
}

func main() {
	outputDirectory := flag.String("out", "", "output directory")
	flag.Parse()
	if *outputDirectory == "" {
		fmt.Fprintln(os.Stderr, "-out is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*outputDirectory, 0o755); err != nil {
		fatal(err)
	}

	binaryBytes, receipt := buildFixture()
	binaryDigest := sha256.Sum256(binaryBytes)
	data := manifest{
		Version:      1,
		Domain:       domain,
		Receipt:      receipt,
		BinaryFile:   "genesis-unicode-max.bin",
		BinaryLength: len(binaryBytes),
		BinarySHA256: hex.EncodeToString(binaryDigest[:]),
		InvalidBounds: map[string]string{
			"delegation_elements": "33 fails",
			"depth":               "33 fails",
			"human_principal":     "257 UTF-8 bytes fail",
			"latency_ms":          "-1 fails",
			"status_code":         "99 and 600 fail",
		},
	}
	manifestBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fatal(err)
	}
	manifestBytes = append(manifestBytes, '\n')

	writeExclusivePair(
		filepath.Join(*outputDirectory, data.BinaryFile), binaryBytes,
		filepath.Join(*outputDirectory, "manifest.json"), manifestBytes,
	)
}

func buildFixture() ([]byte, fixtureReceipt) {
	paramsHash := sha256.Sum256([]byte("{}"))
	zeroHash := make([]byte, 32)
	receipt := fixtureReceipt{
		Seq:             "18446744073709551615",
		TimestampUnixNS: "18446744073709551615",
		HumanPrincipal:  "human-e\u0301",
		AgentKeyID:      "agent-max",
		DelegationChain: []string{},
		Service:         "github",
		Action:          "list_repos",
		ParamsSHA256:    hex.EncodeToString(paramsHash[:]),
		PolicyDecision:  "allow",
		StatusCode:      599,
		LatencyMS:       "9223372036854775807",
		Error:           "",
		PrevHash:        hex.EncodeToString(zeroHash),
		EntryHashNoise:  "11",
		SignerKID:       "signer-v1",
		SignatureNoise:  "22",
	}
	encoded := append([]byte(domain), 0)
	encoded = binary.LittleEndian.AppendUint64(encoded, ^uint64(0))
	encoded = binary.LittleEndian.AppendUint64(encoded, ^uint64(0))
	encoded = appendLP(encoded, receipt.HumanPrincipal)
	encoded = appendLP(encoded, receipt.AgentKeyID)
	encoded = binary.LittleEndian.AppendUint32(encoded, 0)
	encoded = appendLP(encoded, receipt.Service)
	encoded = appendLP(encoded, receipt.Action)
	encoded = append(encoded, paramsHash[:]...)
	encoded = appendLP(encoded, receipt.PolicyDecision)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(receipt.StatusCode))
	encoded = binary.LittleEndian.AppendUint64(encoded, ^uint64(0)>>1)
	encoded = appendLP(encoded, receipt.Error)
	encoded = append(encoded, zeroHash...)
	encoded = appendLP(encoded, receipt.SignerKID)
	return encoded, receipt
}

func appendLP(destination []byte, value string) []byte {
	destination = binary.LittleEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}

func writeExclusive(path string, contents []byte) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		fatal(err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
}

func writeExclusivePair(firstPath string, firstContents []byte, secondPath string, secondContents []byte) {
	created := []string{}
	first, err := os.OpenFile(firstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		fatal(err)
	}
	created = append(created, firstPath)
	second, err := os.OpenFile(secondPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		_ = first.Close()
		removeCreated(created)
		fatal(err)
	}
	created = append(created, secondPath)
	if _, err := first.Write(firstContents); err != nil {
		_ = first.Close()
		_ = second.Close()
		removeCreated(created)
		fatal(err)
	}
	if _, err := second.Write(secondContents); err != nil {
		_ = first.Close()
		_ = second.Close()
		removeCreated(created)
		fatal(err)
	}
	if err := first.Close(); err != nil {
		_ = second.Close()
		removeCreated(created)
		fatal(err)
	}
	if err := second.Close(); err != nil {
		removeCreated(created)
		fatal(err)
	}
}

func removeCreated(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
