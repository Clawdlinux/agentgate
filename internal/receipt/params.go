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
	_, digest, err := ParseAndDigestParams(raw)
	return digest, err
}

// ParseAndDigestParams decodes params once, preserving numeric text for upstream URLs.
// Its digest follows the same RFC 8785 rules as DigestParams.
func ParseAndDigestParams(raw []byte) (map[string]any, [32]byte, error) {
	normalized := normalizeParams(raw)
	if !utf8.Valid(normalized) || !validEscapedSurrogates(normalized) {
		return nil, [32]byte{}, ErrInvalidParams
	}
	params, err := parseJSONObject(normalized)
	if err != nil {
		return nil, [32]byte{}, err
	}

	canonical := appendParamsValue(make([]byte, 0, len(normalized)), params)
	if len(canonical) > maxCanonicalParamsBytes {
		return nil, [32]byte{}, ErrParamsTooLarge
	}
	return params, sha256.Sum256(canonical), nil
}

func normalizeParams(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte("{}")
	}
	return trimmed
}

func parseJSONObject(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	first, err := decoder.Token()
	if err != nil {
		return nil, ErrInvalidParams
	}
	delimiter, ok := first.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, ErrInvalidParams
	}
	params, err := parseObject(decoder, 1)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidParams
	}
	return params, nil
}

func parseObject(decoder *json.Decoder, depth int) (map[string]any, error) {
	if depth > maxParamDepth {
		return nil, ErrInvalidParams
	}
	params := make(map[string]any)
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidParams
		}
		name, ok := nameToken.(string)
		if !ok || containsNoncharacter(name) {
			return nil, ErrInvalidParams
		}
		if _, exists := params[name]; exists {
			return nil, ErrInvalidParams
		}

		valueToken, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidParams
		}
		value, err := parseToken(decoder, valueToken, depth)
		if err != nil {
			return nil, err
		}
		params[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, ErrInvalidParams
	}
	return params, nil
}

func parseArray(decoder *json.Decoder, depth int) ([]any, error) {
	if depth > maxParamDepth {
		return nil, ErrInvalidParams
	}
	values := make([]any, 0)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, ErrInvalidParams
		}
		value, err := parseToken(decoder, token, depth)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return nil, ErrInvalidParams
	}
	return values, nil
}

func parseToken(decoder *json.Decoder, token any, parentDepth int) (any, error) {
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return parseObject(decoder, parentDepth+1)
		case '[':
			return parseArray(decoder, parentDepth+1)
		default:
			return nil, ErrInvalidParams
		}
	case string:
		if containsNoncharacter(value) {
			return nil, ErrInvalidParams
		}
	case json.Number:
		parsed, err := strconv.ParseFloat(value.String(), 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return nil, ErrInvalidParams
		}
	case bool, nil:
		return value, nil
	default:
		return nil, ErrInvalidParams
	}
	return token, nil
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
