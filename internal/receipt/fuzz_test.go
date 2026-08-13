/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"bytes"
	"testing"
)

func FuzzDigestParams(f *testing.F) {
	for _, seed := range [][]byte{[]byte(`{"a":1,"b":2}`), []byte(`{"b":2,"a":1}`), []byte(`{"a":1,"a":2}`), []byte(`null`), []byte(`[]`), []byte(`{"x":1e309}`)} {
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
	f.Add(uint64(1), uint64(1), "human", "agent")
	f.Add(^uint64(0), ^uint64(0), "e\u0301", "key")
	f.Fuzz(func(t *testing.T, sequence, timestamp uint64, human, agent string) {
		value := validReceipt()
		value.Seq, value.TimestampUnixNS = sequence, timestamp
		value.HumanPrincipal, value.AgentKeyID = human, agent
		first, firstErr := CanonicalHashInput(value)
		second, secondErr := CanonicalHashInput(value)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatal("error category changed")
		}
		if firstErr == nil {
			if !bytes.Equal(first, second) || &first[0] == &second[0] {
				t.Fatal("canonical bytes are not independent and repeatable")
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
