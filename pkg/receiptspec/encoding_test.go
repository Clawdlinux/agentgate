package receiptspec

import (
	"strings"
	"testing"
)

func TestCanonicalHashInputExactCapacity(t *testing.T) {
	for _, chain := range [][]string{nil, {}, {"first", "second"}, {strings.Repeat("a", 64)}} {
		receipt := Receipt{
			Seq: 1, TimestampUnixNS: 1, HumanPrincipal: "human", AgentKeyID: "agent",
			Service: "mock", Action: "echo", PolicyDecision: DecisionAllow,
			StatusCode: 200, SignerKID: "signer", DelegationChain: chain,
		}
		encoded, err := CanonicalHashInput(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) != cap(encoded) {
			t.Fatalf("len=%d cap=%d: capacity must match the encoded length", len(encoded), cap(encoded))
		}
	}
}

func BenchmarkCanonicalHashInput(b *testing.B) {
	receipt := Receipt{
		Seq: 1, TimestampUnixNS: 1, HumanPrincipal: "bench-user", AgentKeyID: "agent",
		Service: "mock", Action: "echo", PolicyDecision: DecisionAllow,
		StatusCode: 200, SignerKID: "signer",
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := CanonicalHashInput(receipt); err != nil {
			b.Fatal(err)
		}
	}
}
