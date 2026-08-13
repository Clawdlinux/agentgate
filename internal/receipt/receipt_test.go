/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"reflect"
	"strings"
	"testing"
)

func validReceipt() Receipt {
	return Receipt{
		Seq:             1,
		TimestampUnixNS: 1,
		HumanPrincipal:  "human-1",
		AgentKeyID:      "agent-1",
		Service:         "github",
		Action:          "list_repos",
		PolicyDecision:  "allow",
		StatusCode:      200,
		SignerKID:       "signer-1",
	}
}

func TestReceiptFieldContract(t *testing.T) {
	type expectedField struct {
		name     string
		typeName string
	}
	want := []expectedField{
		{"Seq", "uint64"},
		{"TimestampUnixNS", "uint64"},
		{"HumanPrincipal", "string"},
		{"AgentKeyID", "string"},
		{"DelegationChain", "[]string"},
		{"Service", "string"},
		{"Action", "string"},
		{"ParamsSHA256", "[32]uint8"},
		{"PolicyDecision", "string"},
		{"StatusCode", "int"},
		{"LatencyMS", "int64"},
		{"Error", "string"},
		{"PrevHash", "[32]uint8"},
		{"EntryHash", "[32]uint8"},
		{"SignerKID", "string"},
		{"Signature", "[64]uint8"},
	}

	typeOfReceipt := reflect.TypeOf(Receipt{})
	if typeOfReceipt.NumField() != len(want) {
		t.Fatalf("Receipt has %d fields, want %d", typeOfReceipt.NumField(), len(want))
	}
	for index, expected := range want {
		field := typeOfReceipt.Field(index)
		if field.Name != expected.name || field.Type.String() != expected.typeName {
			t.Errorf("field %d = %s %s, want %s %s", index, field.Name, field.Type, expected.name, expected.typeName)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Receipt)
		valid  bool
	}{
		{name: "minimal valid", valid: true},
		{name: "maximum bounded strings", mutate: func(receipt *Receipt) {
			receipt.HumanPrincipal = strings.Repeat("h", 256)
			receipt.AgentKeyID = strings.Repeat("a", 128)
			receipt.Service = strings.Repeat("s", 64)
			receipt.Action = strings.Repeat("x", 128)
			receipt.SignerKID = strings.Repeat("k", 128)
			receipt.Error = strings.Repeat("e", 64)
			receipt.DelegationChain = make([]string, 32)
			for index := range receipt.DelegationChain {
				receipt.DelegationChain[index] = strings.Repeat("d", 64)
			}
		}, valid: true},
		{name: "exact utf8 bytes preserved", mutate: func(receipt *Receipt) {
			receipt.HumanPrincipal = "e\u0301"
		}, valid: true},
		{name: "empty delegation element allowed", mutate: func(receipt *Receipt) {
			receipt.DelegationChain = []string{""}
		}, valid: true},
		{name: "rate limited decision", mutate: func(receipt *Receipt) {
			receipt.PolicyDecision = "rate_limited"
		}, valid: true},
		{name: "zero sequence", mutate: func(receipt *Receipt) { receipt.Seq = 0 }},
		{name: "zero timestamp", mutate: func(receipt *Receipt) { receipt.TimestampUnixNS = 0 }},
		{name: "status below range", mutate: func(receipt *Receipt) { receipt.StatusCode = 99 }},
		{name: "status above range", mutate: func(receipt *Receipt) { receipt.StatusCode = 600 }},
		{name: "negative latency", mutate: func(receipt *Receipt) { receipt.LatencyMS = -1 }},
		{name: "missing human", mutate: func(receipt *Receipt) { receipt.HumanPrincipal = "" }},
		{name: "missing agent", mutate: func(receipt *Receipt) { receipt.AgentKeyID = "" }},
		{name: "missing service", mutate: func(receipt *Receipt) { receipt.Service = "" }},
		{name: "missing action", mutate: func(receipt *Receipt) { receipt.Action = "" }},
		{name: "missing signer", mutate: func(receipt *Receipt) { receipt.SignerKID = "" }},
		{name: "invalid policy", mutate: func(receipt *Receipt) { receipt.PolicyDecision = "maybe" }},
		{name: "human too long", mutate: func(receipt *Receipt) { receipt.HumanPrincipal = strings.Repeat("h", 257) }},
		{name: "agent too long", mutate: func(receipt *Receipt) { receipt.AgentKeyID = strings.Repeat("a", 129) }},
		{name: "service too long", mutate: func(receipt *Receipt) { receipt.Service = strings.Repeat("s", 65) }},
		{name: "action too long", mutate: func(receipt *Receipt) { receipt.Action = strings.Repeat("a", 129) }},
		{name: "signer too long", mutate: func(receipt *Receipt) { receipt.SignerKID = strings.Repeat("k", 129) }},
		{name: "nul rejected", mutate: func(receipt *Receipt) { receipt.HumanPrincipal = "human\x00one" }},
		{name: "invalid utf8 rejected", mutate: func(receipt *Receipt) { receipt.HumanPrincipal = string([]byte{0xff}) }},
		{name: "too many delegation elements", mutate: func(receipt *Receipt) { receipt.DelegationChain = make([]string, 33) }},
		{name: "delegation element too long", mutate: func(receipt *Receipt) { receipt.DelegationChain = []string{strings.Repeat("d", 65)} }},
		{name: "delegation element non ascii", mutate: func(receipt *Receipt) { receipt.DelegationChain = []string{"delegation-\u00e9"} }},
		{name: "error too long", mutate: func(receipt *Receipt) { receipt.Error = strings.Repeat("e", 65) }},
		{name: "error provider text", mutate: func(receipt *Receipt) { receipt.Error = "token expired" }},
		{name: "error uppercase", mutate: func(receipt *Receipt) { receipt.Error = "TOKEN_EXPIRED" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt := validReceipt()
			if test.mutate != nil {
				test.mutate(&receipt)
			}
			err := Validate(receipt)
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestSnapshot(t *testing.T) {
	receipt := validReceipt()
	receipt.DelegationChain = []string{"first", "second"}
	before := append([]string(nil), receipt.DelegationChain...)

	if err := Validate(receipt); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receipt.DelegationChain, before) {
		t.Fatalf("Validate mutated DelegationChain: got %v want %v", receipt.DelegationChain, before)
	}
}
