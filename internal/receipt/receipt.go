/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package receipt defines AgentGate's deterministic action receipt protocol.
package receipt

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidReceipt = errors.New("receipt: invalid receipt")
	ErrInvalidField   = errors.New("receipt: invalid field")
)

// Receipt is the semantic action receipt in canonical protocol order.
type Receipt struct {
	Seq             uint64
	TimestampUnixNS uint64
	HumanPrincipal  string
	AgentKeyID      string
	DelegationChain []string
	Service         string
	Action          string
	ParamsSHA256    [32]byte
	PolicyDecision  string
	StatusCode      int
	LatencyMS       int64
	Error           string
	PrevHash        [32]byte
	EntryHash       [32]byte
	SignerKID       string
	Signature       [64]byte
}

// Validate checks that receipt satisfies the v1 semantic contract.
func Validate(receipt Receipt) error {
	receipt = snapshot(receipt)

	if receipt.Seq == 0 || receipt.TimestampUnixNS == 0 {
		return ErrInvalidReceipt
	}
	if receipt.StatusCode < 100 || receipt.StatusCode > 599 || receipt.LatencyMS < 0 {
		return ErrInvalidReceipt
	}

	if !validRequiredUTF8(receipt.HumanPrincipal, 256) ||
		!validRequiredUTF8(receipt.AgentKeyID, 128) ||
		!validRequiredUTF8(receipt.Service, 64) ||
		!validRequiredUTF8(receipt.Action, 128) ||
		!validRequiredUTF8(receipt.SignerKID, 128) {
		return ErrInvalidField
	}

	switch receipt.PolicyDecision {
	case "allow", "deny", "rate_limited":
	default:
		return ErrInvalidField
	}

	if len(receipt.DelegationChain) > 32 {
		return ErrInvalidField
	}
	for _, element := range receipt.DelegationChain {
		if len(element) > 64 || !isASCII(element) || strings.IndexByte(element, 0) >= 0 {
			return ErrInvalidField
		}
	}

	if receipt.Error != "" && !validErrorCode(receipt.Error) {
		return ErrInvalidField
	}

	return nil
}

func snapshot(receipt Receipt) Receipt {
	receipt.DelegationChain = slices.Clone(receipt.DelegationChain)
	return receipt
}

func validRequiredUTF8(value string, maximumBytes int) bool {
	return value != "" && len(value) <= maximumBytes && utf8.ValidString(value) && strings.IndexByte(value, 0) < 0
}

func validErrorCode(value string) bool {
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

func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}
