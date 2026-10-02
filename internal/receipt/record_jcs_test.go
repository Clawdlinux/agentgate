/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"encoding/json"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

// HashRecord must agree with the JCS-based DigestParams on the subset it accepts.
func TestHashRecordMatchesDigestParams(t *testing.T) {
	for _, in := range []string{
		`{"b":1,"a":2}`,
		`{"z":{"y":[1,-2,"x",true,null]},"a":"caf\u00e9"}`,
		`{"k":"\u0001\b\t\n\f\r\"\\/<>&"}`,
		`{"😀":2,"ﬁ":1,"a":9007199254740991}`,
	} {
		want, err := DigestParams([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		got, err := receiptspec.HashRecord(json.RawMessage(in))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: HashRecord differs from DigestParams", in)
		}
	}
}
