/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDigestParams_NormalizesEmptyForms(t *testing.T) {
	want := sha256.Sum256([]byte("{}"))
	inputs := [][]byte{nil, {}, []byte(" \n\t"), []byte("null")}
	for _, input := range inputs {
		got, err := DigestParams(input)
		if err != nil {
			t.Fatalf("DigestParams(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("DigestParams(%q) = %x, want %x", input, got, want)
		}
	}
}

func TestDigestParams_EquivalentObjects(t *testing.T) {
	inputs := [][]byte{
		[]byte(`{"b":2,"a":1}`),
		[]byte(" { \n \"a\" : 1.0, \"b\" : 2e0 } "),
	}
	first, err := DigestParams(inputs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := DigestParams(inputs[1])
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("equivalent objects differ: %x != %x", first, second)
	}
}

func TestDigestParams_RejectsInvalidInputs(t *testing.T) {
	depth32 := `{"x":` + strings.Repeat("[", 31) + `0` + strings.Repeat("]", 31) + `}`
	depth33 := `{"x":` + strings.Repeat("[", 32) + `0` + strings.Repeat("]", 32) + `}`

	tests := []struct {
		name  string
		input []byte
		valid bool
	}{
		{name: "depth 32", input: []byte(depth32), valid: true},
		{name: "depth 33", input: []byte(depth33)},
		{name: "array root", input: []byte(`[]`)},
		{name: "scalar root", input: []byte(`1`)},
		{name: "trailing value", input: []byte(`{} {}`)},
		{name: "literal duplicate", input: []byte(`{"a":1,"a":2}`)},
		{name: "escaped duplicate", input: []byte(`{"a":1,"\u0061":2}`)},
		{name: "invalid utf8", input: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}},
		{name: "lone high surrogate", input: []byte(`{"x":"\uD800"}`)},
		{name: "lone low surrogate", input: []byte(`{"x":"\uDC00"}`)},
		{name: "reversed surrogate pair", input: []byte(`{"x":"\uDC00\uD800"}`)},
		{name: "invalid surrogate pair", input: []byte(`{"x":"\uD800\u0041"}`)},
		{name: "valid surrogate pair", input: []byte(`{"x":"\uD83D\uDE00"}`), valid: true},
		{name: "escaped noncharacter", input: []byte(`{"x":"\uFDD0"}`)},
		{name: "raw noncharacter", input: []byte("{\"x\":\"\uFDD0\"}")},
		{name: "number overflow", input: []byte(`{"x":1e309}`)},
		{name: "representable large integer", input: []byte(`{"x":9007199254740992}`), valid: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DigestParams(test.input)
			if test.valid && err != nil {
				t.Fatalf("DigestParams() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("DigestParams() error = nil, want rejection")
			}
		})
	}
}

func TestDigestParams_CanonicalSizeBoundary(t *testing.T) {
	const objectOverhead = len(`{"x":""}`)
	exact := []byte(`{"x":"` + strings.Repeat("a", maxCanonicalParamsBytes-objectOverhead) + `"}`)
	tooLarge := []byte(`{"x":"` + strings.Repeat("a", maxCanonicalParamsBytes-objectOverhead+1) + `"}`)

	if _, err := DigestParams(exact); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}
	if _, err := DigestParams(tooLarge); !errors.Is(err, ErrParamsTooLarge) {
		t.Fatalf("oversize error = %v, want ErrParamsTooLarge", err)
	}
}

func TestDigestParams_DoesNotMutateInput(t *testing.T) {
	raw := []byte(` {"b":2,"a":1} `)
	want := bytes.Clone(raw)
	if _, err := DigestParams(raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("input mutated: got %q want %q", raw, want)
	}
}

func TestPrivacy(t *testing.T) {
	sentinels := []string{"oauth_secret_123", "provider said card declined", "raw_request_body"}
	for _, sentinel := range sentinels {
		_, err := DigestParams([]byte(`{"x":"` + sentinel + `"} trailing`))
		if err == nil {
			t.Fatal("invalid JSON accepted")
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("error leaked sentinel %q: %v", sentinel, err)
		}
	}

	typeOfReceipt := reflectTypeOfReceipt()
	for _, fieldName := range []string{"RawParams", "CanonicalParams", "OAuthToken", "UpstreamBody", "ProviderError"} {
		if _, exists := typeOfReceipt.FieldByName(fieldName); exists {
			t.Fatalf("Receipt contains prohibited field %s", fieldName)
		}
	}
}

func reflectTypeOfReceipt() reflect.Type {
	return reflect.TypeOf(Receipt{})
}
