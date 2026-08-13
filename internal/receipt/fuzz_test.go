/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"bytes"
	"strings"
	"testing"
)

func FuzzDigestParams(f *testing.F) {
	depth32 := []byte(`{"x":` + strings.Repeat("[", 31) + `0` + strings.Repeat("]", 31) + `}`)
	depth33 := []byte(`{"x":` + strings.Repeat("[", 32) + `0` + strings.Repeat("]", 32) + `}`)
	exactLimit := []byte(`{"x":"` + strings.Repeat("a", maxCanonicalParamsBytes-len(`{"x":""}`)) + `"}`)
	overLimit := []byte(`{"x":"` + strings.Repeat("a", maxCanonicalParamsBytes-len(`{"x":""}`)+1) + `"}`)
	for _, seed := range [][]byte{
		[]byte(`{"a":1,"b":2}`),
		[]byte(`{"b":2,"a":1}`),
		[]byte(`{"a":1,"a":2}`),
		[]byte(`{"a":1,"\u0061":2}`),
		[]byte("{\"x\":\"\xff\"}"),
		[]byte(`{"x":"\uD800"}`),
		[]byte(`{"x":"\uDC00"}`),
		[]byte(`{"x":"\uDC00\uD800"}`),
		[]byte(`{"x":"\uD83D\uDE00"}`),
		[]byte(`{"x":"\uFDD0"}`),
		[]byte("{\"x\":\"\uFDD0\"}"),
		depth32,
		depth33,
		exactLimit,
		overLimit,
		[]byte(`{"x":9007199254740992}`),
		[]byte(`{"x":1e309}`),
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`{} {}`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		first, firstErr := DigestParams(raw)
		second, secondErr := DigestParams(raw)
		if (firstErr == nil) != (secondErr == nil) || first != second {
			t.Fatal("DigestParams is not deterministic")
		}
	})
}

func FuzzCanonicalHashInput(f *testing.F) {
	f.Add(uint64(1), uint64(1), "human", "agent", "github", "list_repos", "allow", 200, int64(0), "", "signer", "delegation")
	f.Add(^uint64(0), ^uint64(0), "e\u0301", "key", "slack", "post_message", "rate_limited", 599, int64(^uint64(0)>>1), "timeout", strings.Repeat("k", 128), strings.Repeat("d", 64))
	f.Fuzz(func(t *testing.T, sequence, timestamp uint64, human, agent, service, action, policy string, status int, latency int64, errorCode, signer, delegation string) {
		value := validReceipt()
		value.Seq, value.TimestampUnixNS = sequence, timestamp
		value.HumanPrincipal, value.AgentKeyID = human, agent
		value.Service, value.Action = service, action
		value.PolicyDecision = policy
		value.StatusCode = status
		value.LatencyMS = latency
		value.Error = errorCode
		value.SignerKID = signer
		value.DelegationChain = []string{delegation}
		first, firstErr := CanonicalHashInput(value)
		second, secondErr := CanonicalHashInput(value)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatal("error category changed")
		}
		if firstErr == nil {
			if !bytes.Equal(first, second) || &first[0] == &second[0] {
				t.Fatal("canonical bytes are not independent and repeatable")
			}
			frozen := append([]byte(nil), first...)
			value.DelegationChain[0] += "x"
			if !bytes.Equal(first, frozen) {
				t.Fatal("returned preimage changed after caller slice mutation")
			}
			value.DelegationChain[0] = delegation
			hash, err := ComputeEntryHash(value)
			if err != nil {
				t.Fatal(err)
			}
			if repeatHash, err := ComputeEntryHash(value); err != nil || hash != repeatHash {
				t.Fatal("entry hash is not repeatable")
			}
			for _, mutation := range []func(*Receipt){
				func(receipt *Receipt) { receipt.Seq++ },
				func(receipt *Receipt) { receipt.TimestampUnixNS++ },
				func(receipt *Receipt) { receipt.HumanPrincipal += "x" },
				func(receipt *Receipt) { receipt.AgentKeyID += "x" },
				func(receipt *Receipt) { receipt.DelegationChain = append(receipt.DelegationChain, "extra") },
				func(receipt *Receipt) { receipt.DelegationChain[0] += "x" },
				func(receipt *Receipt) { receipt.Service += "x" },
				func(receipt *Receipt) { receipt.Action += "x" },
				func(receipt *Receipt) { receipt.ParamsSHA256[0]++ },
				func(receipt *Receipt) { receipt.PolicyDecision = "deny" },
				func(receipt *Receipt) { receipt.StatusCode-- },
				func(receipt *Receipt) { receipt.LatencyMS++ },
				func(receipt *Receipt) { receipt.Error = "error" },
				func(receipt *Receipt) { receipt.PrevHash[0]++ },
				func(receipt *Receipt) { receipt.SignerKID += "x" },
			} {
				mutated := value
				mutated.DelegationChain = append([]string(nil), value.DelegationChain...)
				mutation(&mutated)
				mutatedBytes, err := CanonicalHashInput(mutated)
				if err == nil && bytes.Equal(first, mutatedBytes) {
					t.Fatal("included-field mutation did not change preimage")
				}
			}
			derived := value
			derived.EntryHash[0] = 1
			derived.Signature[0] = 1
			derivedBytes, err := CanonicalHashInput(derived)
			if err != nil || !bytes.Equal(first, derivedBytes) {
				t.Fatal("derived fields changed preimage")
			}
		}
	})
}
