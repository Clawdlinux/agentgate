package receipt

import (
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func BenchmarkLedgerPreflight(b *testing.B) {
	preflight := testDraft("mock", "echo").receipt()
	preflight.Seq, preflight.TimestampUnixNS, preflight.SignerKID = 1, 1, "preflight"
	b.Run("canonical", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := receiptspec.CanonicalHashInput(preflight); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("validate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := receiptspec.Validate(preflight); err != nil {
				b.Fatal(err)
			}
		}
	})
}
