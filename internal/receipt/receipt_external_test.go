/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Clawdlinux/agentgate/internal/receipt"
)

func referenceReceipt() receipt.Receipt {
	value := receipt.Receipt{Seq: 1, TimestampUnixNS: 2, HumanPrincipal: "human-e\u0301", AgentKeyID: "agent", Service: "github", Action: "list_repos", PolicyDecision: "allow", StatusCode: 200, LatencyMS: 3, Error: "", SignerKID: "kid"}
	for index := range value.ParamsSHA256 {
		value.ParamsSHA256[index] = byte(index)
		value.PrevHash[index] = byte(255 - index)
	}
	return value
}

func referenceEncode(value receipt.Receipt) []byte {
	encoded := append([]byte(receipt.HashDomainV1), 0)
	encoded = binary.LittleEndian.AppendUint64(encoded, value.Seq)
	encoded = binary.LittleEndian.AppendUint64(encoded, value.TimestampUnixNS)
	encoded = referenceLP(encoded, value.HumanPrincipal)
	encoded = referenceLP(encoded, value.AgentKeyID)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(value.DelegationChain)))
	for _, element := range value.DelegationChain {
		encoded = referenceLP(encoded, element)
	}
	encoded = referenceLP(encoded, value.Service)
	encoded = referenceLP(encoded, value.Action)
	encoded = append(encoded, value.ParamsSHA256[:]...)
	encoded = referenceLP(encoded, value.PolicyDecision)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(int32(value.StatusCode)))
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(value.LatencyMS))
	encoded = referenceLP(encoded, value.Error)
	encoded = append(encoded, value.PrevHash[:]...)
	encoded = referenceLP(encoded, value.SignerKID)
	return encoded
}

func referenceLP(destination []byte, value string) []byte {
	destination = binary.LittleEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}

func TestReferenceEncoderAgreement(t *testing.T) {
	for _, value := range []receipt.Receipt{referenceReceipt(), maximumReceipt()} {
		validateReferenceAssumptions(t, value)
		production, err := receipt.CanonicalHashInput(value)
		if err != nil {
			t.Fatal(err)
		}
		if reference := referenceEncode(value); !bytes.Equal(production, reference) {
			t.Fatalf("encoder mismatch\nproduction: %x\nreference:  %x", production, reference)
		}
	}
}

func validateReferenceAssumptions(t *testing.T, value receipt.Receipt) {
	t.Helper()
	if value.Seq == 0 || value.TimestampUnixNS == 0 {
		t.Fatal("reference receipt uses zero sequence or timestamp")
	}
	if !validReferenceUTF8(value.HumanPrincipal, 256) || !validReferenceUTF8(value.AgentKeyID, 128) || !validReferenceUTF8(value.Service, 64) || !validReferenceUTF8(value.Action, 128) || !validReferenceUTF8(value.SignerKID, 128) {
		t.Fatal("reference receipt uses an empty required string")
	}
	if value.PolicyDecision != "allow" && value.PolicyDecision != "deny" && value.PolicyDecision != "rate_limited" {
		t.Fatalf("reference receipt uses invalid policy decision %q", value.PolicyDecision)
	}
	if value.StatusCode < 100 || value.StatusCode > 599 {
		t.Fatalf("reference receipt uses invalid status %d", value.StatusCode)
	}
	if value.LatencyMS < 0 {
		t.Fatalf("reference receipt uses negative latency %d", value.LatencyMS)
	}
	if len(value.DelegationChain) > 32 {
		t.Fatalf("reference receipt uses %d delegation elements", len(value.DelegationChain))
	}
	for _, element := range value.DelegationChain {
		if len(element) > 64 || strings.IndexByte(element, 0) >= 0 || !isReferenceASCII(element) {
			t.Fatalf("reference receipt uses invalid delegation element %q", element)
		}
	}
	if value.Error != "" && !validReferenceErrorCode(value.Error) {
		t.Fatalf("reference receipt uses invalid error code %q", value.Error)
	}
}

func maximumReceipt() receipt.Receipt {
	value := referenceReceipt()
	value.Seq = ^uint64(0)
	value.TimestampUnixNS = ^uint64(0)
	value.HumanPrincipal = strings.Repeat("h", 256)
	value.AgentKeyID = strings.Repeat("a", 128)
	value.DelegationChain = make([]string, 32)
	for index := range value.DelegationChain {
		value.DelegationChain[index] = strings.Repeat("d", 64)
	}
	value.Service = strings.Repeat("s", 64)
	value.Action = strings.Repeat("x", 128)
	value.StatusCode = 599
	value.LatencyMS = int64(^uint64(0) >> 1)
	value.Error = strings.Repeat("e", 64)
	value.SignerKID = strings.Repeat("k", 128)
	return value
}

func validReferenceUTF8(value string, maximumBytes int) bool {
	return value != "" && len(value) <= maximumBytes && utf8.ValidString(value) && strings.IndexByte(value, 0) < 0
}

func isReferenceASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}

func validReferenceErrorCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}
