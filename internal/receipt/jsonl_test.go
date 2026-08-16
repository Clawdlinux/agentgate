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

func TestJSONL_RoundTrip(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestLedgerAndStore(t)
	receipts := appendN(t, ledger, 3)

	for _, want := range receipts {
		line, err := MarshalJSONLReceipt(want)
		if err != nil {
			t.Fatalf("MarshalJSONLReceipt: %v", err)
		}
		got, err := ParseJSONLReceipt(line)
		if err != nil {
			t.Fatalf("ParseJSONLReceipt: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, want)
		}
	}
}

func TestJSONL_RejectsUnsupportedFormatVersion(t *testing.T) {
	t.Parallel()
	ledger, _ := newTestLedgerAndStore(t)
	r := appendN(t, ledger, 1)[0]

	line, err := MarshalJSONLReceipt(r)
	if err != nil {
		t.Fatalf("MarshalJSONLReceipt: %v", err)
	}
	// Bump the version field the same way a byte-for-byte JSON replace
	// would: "format_version":1 -> "format_version":2.
	tampered := []byte(strings.Replace(string(line), `"format_version":1`, `"format_version":2`, 1))

	if _, err := ParseJSONLReceipt(tampered); err == nil {
		t.Fatal("expected an unsupported format_version error")
	}
}
