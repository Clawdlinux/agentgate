/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt_test

import (
	"bytes"
	"encoding/binary"
	"testing"

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
		if err := receipt.Validate(value); err != nil {
			t.Fatal(err)
		}
		production, err := receipt.CanonicalHashInput(value)
		if err != nil {
			t.Fatal(err)
		}
		if reference := referenceEncode(value); !bytes.Equal(production, reference) {
			t.Fatalf("encoder mismatch\nproduction: %x\nreference:  %x", production, reference)
		}
	}
}

func maximumReceipt() receipt.Receipt {
	value := referenceReceipt()
	value.Seq = ^uint64(0)
	value.TimestampUnixNS = ^uint64(0)
	value.StatusCode = 599
	value.LatencyMS = int64(^uint64(0) >> 1)
	return value
}
