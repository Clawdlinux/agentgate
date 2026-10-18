/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import (
	"strings"
	"testing"
)

func TestEmptyExportExpectedHead(t *testing.T) {
	for _, name := range []string{"valid-empty-genesis", "valid-empty-anchor", "manifest-empty-endpoint-mismatch"} {
		t.Run(name, func(t *testing.T) {
			bundle, err := ReadJSONL(strings.NewReader(string(readVectorFile(t, name+".jsonl"))))
			if err != nil {
				t.Fatal(err)
			}
			for _, mismatch := range []string{"none", "seq", "hash"} {
				t.Run(mismatch, func(t *testing.T) {
					expected := ExpectedHead{Seq: bundle.Manifest.AnchorSeq, EntryHash: bundle.Manifest.AnchorHash}
					switch mismatch {
					case "seq":
						expected.Seq++
					case "hash":
						expected.EntryHash[0] ^= 1
					}
					result, err := VerifyBundle(bundle, nil, &expected)
					wantOK := name != "manifest-empty-endpoint-mismatch" && mismatch == "none"
					if err != nil || result.OK != wantOK || result.Complete != wantOK {
						t.Fatalf("result=%+v err=%v wantOK=%v", result, err, wantOK)
					}
					if !wantOK && result.Reason != ReasonExpectedHeadMismatch {
						t.Fatalf("reason=%s", result.Reason)
					}
				})
			}
		})
	}
}

func TestManifestChecksCannotBeOverridden(t *testing.T) {
	for _, name := range []string{
		"manifest-retired-signer-full-range", "manifest-signer-before-window",
		"manifest-count-mismatch", "manifest-negative-count", "manifest-reversed-range",
		"manifest-endpoint-mismatch", "manifest-head-hash-mismatch", "manifest-first-entry-hash-mismatch",
	} {
		t.Run(name, func(t *testing.T) {
			bundle, err := ReadJSONL(strings.NewReader(string(readVectorFile(t, name+".jsonl"))))
			if err != nil {
				t.Fatal(err)
			}
			last := bundle.Receipts[len(bundle.Receipts)-1]
			result, err := VerifyBundle(bundle, nil, &ExpectedHead{Seq: last.Seq, EntryHash: last.EntryHash})
			if err != nil || result.OK || result.Complete || !strings.HasPrefix(result.Reason, "manifest_") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if name != "manifest-first-entry-hash-mismatch" {
				err := VerifyManifest(*bundle.Manifest, bundle.Keys, bundle.Keys)
				if err == nil || err.Error() != result.Reason {
					t.Fatalf("VerifyManifest error=%v, want reason=%s", err, result.Reason)
				}
			}
		})
	}
}
