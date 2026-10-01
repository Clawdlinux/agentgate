/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestCanonicalRecord(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"key order", `{"b":1,"a":2}`, `{"a":2,"b":1}`},
		{"whitespace", " { \"a\" : [ 1 , 2 ] } ", `{"a":[1,2]}`},
		{"nested", `{"z":{"y":{"x":true,"w":null}},"a":[]}`, `{"a":[],"z":{"y":{"w":null,"x":true}}}`},
		{"unicode literal", `{"k":"caf\u00e9 \u20ac"}`, `{"k":"café €"}`},
		{"html not escaped", `{"k":"<a&b>"}`, `{"k":"<a&b>"}`},
		{"controls", `{"k":"\u0001\b\t\n\f\r\"\\/"}`, `{"k":"\u0001\b\t\n\f\r\"\\/"}`},
		{"surrogate pair", `{"k":"\ud83d\ude00"}`, `{"k":"😀"}`},
		// RFC 8785 sorts by UTF-16 code units: U+1F600 (D83D) sorts before U+FB01 (FB01).
		{"utf16 key order", `{"ﬁ":1,"😀":2}`, `{"😀":2,"ﬁ":1}`},
		{"max safe integers", `{"a":9007199254740991,"b":-9007199254740991,"c":0}`, `{"a":9007199254740991,"b":-9007199254740991,"c":0}`},
		{"scalar top level", `"x"`, `"x"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := receiptspec.CanonicalRecord(json.RawMessage(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCanonicalRecordRejects(t *testing.T) {
	cases := []struct {
		in   string
		want error
	}{
		{`{"a":1.5}`, receiptspec.ErrUnsupportedNumber},
		{`{"a":1e3}`, receiptspec.ErrUnsupportedNumber},
		{`{"a":1.0}`, receiptspec.ErrUnsupportedNumber},
		{`{"a":-0}`, receiptspec.ErrUnsupportedNumber},
		{`{"a":9007199254740992}`, receiptspec.ErrUnsupportedNumber},
		{`{"a":1,"a":2}`, receiptspec.ErrInvalidRecord},
		{`{"a":"\ud800"}`, receiptspec.ErrInvalidRecord},
		{`{"a":"\udc00"}`, receiptspec.ErrInvalidRecord},
		{"{\"a\":\"\xff\"}", receiptspec.ErrInvalidRecord},
		{`{"a":1} {}`, receiptspec.ErrInvalidRecord},
		{`{"a":`, receiptspec.ErrInvalidRecord},
		{`[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]`, receiptspec.ErrInvalidRecord},
	}
	for _, tc := range cases {
		if _, err := receiptspec.CanonicalRecord(json.RawMessage(tc.in)); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.in, err, tc.want)
		}
	}
}

func TestHashRecordStructAndMapAgree(t *testing.T) {
	type record struct {
		Outcome     string `json:"outcome"`
		Probability string `json:"probability"`
		ScoreMicros int64  `json:"score_micros"`
	}
	a, err := receiptspec.HashRecord(record{Outcome: "approved", Probability: "0.875", ScoreMicros: 875000})
	if err != nil {
		t.Fatal(err)
	}
	b, err := receiptspec.HashRecord(map[string]any{"score_micros": 875000, "probability": "0.875", "outcome": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("struct and map with equal content hash differently")
	}
	if _, err := receiptspec.HashRecord(map[string]float64{"p": 0.5}); !errors.Is(err, receiptspec.ErrUnsupportedNumber) {
		t.Fatalf("float err = %v", err)
	}
}

func TestVerifyRecordBinding(t *testing.T) {
	record := []byte(`{"outcome":"edited","record_version":1}`)
	hash, err := receiptspec.HashRecord(json.RawMessage(record))
	if err != nil {
		t.Fatal(err)
	}
	r := receiptspec.Receipt{ParamsSHA256: hash}
	if err := receiptspec.VerifyRecordBinding(r, []byte(" {\"record_version\": 1, \"outcome\": \"edited\"} ")); err != nil {
		t.Fatal(err)
	}
	if err := receiptspec.VerifyRecordBinding(r, []byte(`{"outcome":"approved","record_version":1}`)); !errors.Is(err, receiptspec.ErrRecordMismatch) {
		t.Fatalf("err = %v, want ErrRecordMismatch", err)
	}
}

func FuzzCanonicalRecord(f *testing.F) {
	for _, seed := range []string{`{"b":1,"a":[true,null,"x"]}`, `{"😀":{"ﬁ":-1}}`, `"\u0007"`, `[]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := receiptspec.CanonicalRecord(json.RawMessage(in))
		if err != nil {
			return
		}
		again, err := receiptspec.CanonicalRecord(json.RawMessage(out))
		if err != nil || string(again) != string(out) {
			t.Fatalf("not idempotent: %q -> %q (%v)", out, again, err)
		}
		if !json.Valid(out) {
			t.Fatalf("invalid JSON output %q", out)
		}
	})
}
