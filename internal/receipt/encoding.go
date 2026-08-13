/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"crypto/sha256"
	"encoding/binary"
)

// HashDomainV1 separates AgentGate receipt hashes from other protocols and versions.
const HashDomainV1 = "agentgate.receipt.hash.v1"

// CanonicalHashInput returns the validated v1 entry-hash preimage.
func CanonicalHashInput(receipt Receipt) ([]byte, error) {
	receipt = snapshot(receipt)
	if err := Validate(receipt); err != nil {
		return nil, err
	}

	encoded := make([]byte, 0, 3110)
	encoded = append(encoded, HashDomainV1...)
	encoded = append(encoded, 0)
	encoded = binary.LittleEndian.AppendUint64(encoded, receipt.Seq)
	encoded = binary.LittleEndian.AppendUint64(encoded, receipt.TimestampUnixNS)
	encoded = appendLP(encoded, receipt.HumanPrincipal)
	encoded = appendLP(encoded, receipt.AgentKeyID)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(len(receipt.DelegationChain)))
	for _, element := range receipt.DelegationChain {
		encoded = appendLP(encoded, element)
	}
	encoded = appendLP(encoded, receipt.Service)
	encoded = appendLP(encoded, receipt.Action)
	encoded = append(encoded, receipt.ParamsSHA256[:]...)
	encoded = appendLP(encoded, receipt.PolicyDecision)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(int32(receipt.StatusCode)))
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(receipt.LatencyMS))
	encoded = appendLP(encoded, receipt.Error)
	encoded = append(encoded, receipt.PrevHash[:]...)
	encoded = appendLP(encoded, receipt.SignerKID)
	return encoded, nil
}

// ComputeEntryHash returns SHA-256 over the complete canonical hash input.
func ComputeEntryHash(receipt Receipt) ([32]byte, error) {
	encoded, err := CanonicalHashInput(receipt)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func appendLP(destination []byte, value string) []byte {
	destination = binary.LittleEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}
