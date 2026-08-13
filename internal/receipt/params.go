/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/gowebpki/jcs"
)

const (
	maxParamDepth           = 32
	maxCanonicalParamsBytes = 1 << 20
)

var (
	ErrInvalidParams  = errors.New("receipt: invalid params")
	ErrParamsTooLarge = errors.New("receipt: params too large")
)

// DigestParams validates and canonicalizes a JSON object, then returns its SHA-256 digest.
func DigestParams(raw []byte) ([32]byte, error) {
	normalized := normalizeParams(raw)
	if !utf8.Valid(normalized) || !validEscapedSurrogates(normalized) {
		return [32]byte{}, ErrInvalidParams
	}
	if err := validateJSONObject(normalized); err != nil {
		return [32]byte{}, err
	}

	canonical, err := jcs.Transform(bytes.Clone(normalized))
	if err != nil {
		return [32]byte{}, ErrInvalidParams
	}
	if len(canonical) > maxCanonicalParamsBytes {
		return [32]byte{}, ErrParamsTooLarge
	}
	return sha256.Sum256(canonical), nil
}

func normalizeParams(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte("{}")
	}
	return bytes.Clone(trimmed)
}

func validateJSONObject(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	first, err := decoder.Token()
	if err != nil {
		return ErrInvalidParams
	}
	delimiter, ok := first.(json.Delim)
	if !ok || delimiter != '{' {
		return ErrInvalidParams
	}
	if err := validateObject(decoder, 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidParams
	}
	return nil
}

func validateObject(decoder *json.Decoder, depth int) error {
	if depth > maxParamDepth {
		return ErrInvalidParams
	}
	names := make(map[string]struct{})
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return ErrInvalidParams
		}
		name, ok := nameToken.(string)
		if !ok || containsNoncharacter(name) {
			return ErrInvalidParams
		}
		if _, exists := names[name]; exists {
			return ErrInvalidParams
		}
		names[name] = struct{}{}

		valueToken, err := decoder.Token()
		if err != nil || validateToken(decoder, valueToken, depth) != nil {
			return ErrInvalidParams
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return ErrInvalidParams
	}
	return nil
}

func validateArray(decoder *json.Decoder, depth int) error {
	if depth > maxParamDepth {
		return ErrInvalidParams
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil || validateToken(decoder, token, depth) != nil {
			return ErrInvalidParams
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return ErrInvalidParams
	}
	return nil
}

func validateToken(decoder *json.Decoder, token any, parentDepth int) error {
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return validateObject(decoder, parentDepth+1)
		case '[':
			return validateArray(decoder, parentDepth+1)
		default:
			return ErrInvalidParams
		}
	case string:
		if containsNoncharacter(value) {
			return ErrInvalidParams
		}
	case json.Number:
		parsed, err := strconv.ParseFloat(value.String(), 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return ErrInvalidParams
		}
	case bool, nil:
		return nil
	default:
		return ErrInvalidParams
	}
	return nil
}

func containsNoncharacter(value string) bool {
	for _, character := range value {
		if character >= 0xfdd0 && character <= 0xfdef {
			return true
		}
		if character&0xffff == 0xfffe || character&0xffff == 0xffff {
			return true
		}
	}
	return false
}

func validEscapedSurrogates(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			first, ok := parseHexQuad(raw, index+2)
			if !ok {
				return false
			}
			if first >= 0xdc00 && first <= 0xdfff {
				return false
			}
			if first >= 0xd800 && first <= 0xdbff {
				if index+12 > len(raw) || raw[index+6] != '\\' || raw[index+7] != 'u' {
					return false
				}
				second, ok := parseHexQuad(raw, index+8)
				if !ok || second < 0xdc00 || second > 0xdfff {
					return false
				}
				index += 11
				continue
			}
			index += 5
		}
	}
	return true
}

func parseHexQuad(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(raw[start:start+4]), 16, 16)
	return uint16(value), err == nil
}
