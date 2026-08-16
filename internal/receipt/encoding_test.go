/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"testing"
)

func encodingReceipt() Receipt {
	receipt := validReceipt()
	receipt.Seq = 0x0102030405060708
	receipt.TimestampUnixNS = 0x1112131415161718
	receipt.HumanPrincipal = "human-\u00e9"
	receipt.AgentKeyID = "agent-key"
	receipt.DelegationChain = []string{"first", "second"}
	receipt.Service = "github"
	receipt.Action = "list_repos"
	for index := range receipt.ParamsSHA256 {
		receipt.ParamsSHA256[index] = byte(index + 1)
		receipt.PrevHash[index] = byte(0x80 + index)
	}
	receipt.PolicyDecision = "allow"
	receipt.StatusCode = 599
	receipt.LatencyMS = 0x2122232425262728
	receipt.Error = "upstream_failed"
	receipt.SignerKID = "signer-v1"
	return receipt
}

func TestCanonicalHashInput(t *testing.T) {
	receipt := encodingReceipt()
	got, err := CanonicalHashInput(receipt)
	if err != nil {
		t.Fatal(err)
	}

	want := append([]byte(HashDomainV1), 0)
	want = binary.LittleEndian.AppendUint64(want, receipt.Seq)
	want = binary.LittleEndian.AppendUint64(want, receipt.TimestampUnixNS)
	want = testAppendLP(want, receipt.HumanPrincipal)
	want = testAppendLP(want, receipt.AgentKeyID)
	want = binary.LittleEndian.AppendUint32(want, uint32(len(receipt.DelegationChain)))
	for _, element := range receipt.DelegationChain {
		want = testAppendLP(want, element)
	}
	want = testAppendLP(want, receipt.Service)
	want = testAppendLP(want, receipt.Action)
	want = append(want, receipt.ParamsSHA256[:]...)
	want = testAppendLP(want, receipt.PolicyDecision)
	want = binary.LittleEndian.AppendUint32(want, uint32(int32(receipt.StatusCode)))
	want = binary.LittleEndian.AppendUint64(want, uint64(receipt.LatencyMS))
	want = testAppendLP(want, receipt.Error)
	want = append(want, receipt.PrevHash[:]...)
	want = testAppendLP(want, receipt.SignerKID)

	if !bytes.Equal(got, want) {
		t.Fatalf("CanonicalHashInput() mismatch\n got: %x\nwant: %x", got, want)
	}
	if !bytes.HasPrefix(got, append([]byte("agentgate.receipt.hash.v1"), 0)) {
		t.Fatalf("missing domain prefix: %x", got[:26])
	}
}

func TestCanonicalHashInput_FieldMutations(t *testing.T) {
	base := encodingReceipt()
	baseBytes, err := CanonicalHashInput(base)
	if err != nil {
		t.Fatal(err)
	}

	included := []struct {
		name   string
		mutate func(*Receipt)
	}{
		{"seq", func(receipt *Receipt) { receipt.Seq++ }},
		{"timestamp", func(receipt *Receipt) { receipt.TimestampUnixNS++ }},
		{"human", func(receipt *Receipt) { receipt.HumanPrincipal += "x" }},
		{"agent", func(receipt *Receipt) { receipt.AgentKeyID += "x" }},
		{"delegation", func(receipt *Receipt) { receipt.DelegationChain[0] += "x" }},
		{"service", func(receipt *Receipt) { receipt.Service += "x" }},
		{"action", func(receipt *Receipt) { receipt.Action += "x" }},
		{"params hash", func(receipt *Receipt) { receipt.ParamsSHA256[0]++ }},
		{"policy", func(receipt *Receipt) { receipt.PolicyDecision = "deny" }},
		{"status", func(receipt *Receipt) { receipt.StatusCode-- }},
		{"latency", func(receipt *Receipt) { receipt.LatencyMS++ }},
		{"error", func(receipt *Receipt) { receipt.Error = "timeout" }},
		{"prev hash", func(receipt *Receipt) { receipt.PrevHash[0]++ }},
		{"signer", func(receipt *Receipt) { receipt.SignerKID += "x" }},
	}
	for _, test := range included {
		t.Run(test.name, func(t *testing.T) {
			mutated := base
			mutated.DelegationChain = append([]string(nil), base.DelegationChain...)
			test.mutate(&mutated)
			got, err := CanonicalHashInput(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(got, baseBytes) {
				t.Fatal("included field mutation did not change preimage")
			}
		})
	}

	derived := base
	derived.EntryHash[0] = 1
	derived.Signature[0] = 1
	got, err := CanonicalHashInput(derived)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, baseBytes) {
		t.Fatal("derived fields changed preimage")
	}
}

func TestCanonicalHashInput_FreshSnapshot(t *testing.T) {
	receipt := encodingReceipt()
	before := append([]string(nil), receipt.DelegationChain...)
	first, err := CanonicalHashInput(receipt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalHashInput(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || &first[0] == &second[0] {
		t.Fatal("calls must return equal independent byte slices")
	}
	first[0] ^= 0xff
	if bytes.Equal(first, second) {
		t.Fatal("returned slices alias")
	}
	if !reflect.DeepEqual(receipt.DelegationChain, before) {
		t.Fatal("caller delegation mutated")
	}
}

func TestCanonicalHashInput_ValidatesFirst(t *testing.T) {
	receipt := encodingReceipt()
	receipt.StatusCode = 99
	if _, err := CanonicalHashInput(receipt); err == nil {
		t.Fatal("invalid receipt encoded")
	}
}

func TestComputeEntryHash(t *testing.T) {
	receipt := encodingReceipt()
	input, err := CanonicalHashInput(receipt)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(input)
	got, err := ComputeEntryHash(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ComputeEntryHash() = %x, want %x", got, want)
	}
}

func testAppendLP(destination []byte, value string) []byte {
	destination = binary.LittleEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}
