/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	maxRecordDepth = 32
	maxRecordBytes = 1 << 20
	maxSafeInteger = 1<<53 - 1
)

var (
	// ErrInvalidRecord means a decision record is not valid JSON, has
	// duplicate keys, lone surrogate escapes, or nests too deeply.
	ErrInvalidRecord = errors.New("receipt: invalid decision record")
	// ErrUnsupportedNumber means a record contains a number that is not an
	// integer in [-(2^53-1), 2^53-1]. Encode fractions as decimal strings.
	ErrUnsupportedNumber = errors.New("receipt: unsupported number in decision record")
	// ErrRecordMismatch means a record's hash differs from the receipt's ParamsSHA256.
	ErrRecordMismatch = errors.New("receipt: decision record does not match params_sha256")
)

var integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// CanonicalRecord returns the canonical JSON form of v used by HashRecord.
// It follows RFC 8785 (JCS) for the subset it accepts: object keys sorted by
// UTF-16 code units, no insignificant whitespace, minimal string escaping.
// Numbers must be integers within +/-(2^53-1); floats, exponents, and -0 are
// rejected. A json.RawMessage is canonicalized as-is; any other value is
// first encoded with encoding/json.
func CanonicalRecord(v any) ([]byte, error) {
	raw, ok := v.(json.RawMessage)
	if !ok {
		var err error
		if raw, err = json.Marshal(v); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
		}
	}
	if !utf8.Valid(raw) || !validSurrogateEscapes(raw) {
		return nil, ErrInvalidRecord
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, ErrInvalidRecord
	}
	out, err := canonicalValue(dec, tok, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidRecord
	}
	if len(out) > maxRecordBytes {
		return nil, ErrInvalidRecord
	}
	return out, nil
}

// HashRecord returns SHA-256 of CanonicalRecord(v). A non-gateway producer
// puts this value in Receipt.ParamsSHA256 to bind a decision record.
func HashRecord(v any) ([32]byte, error) {
	canonical, err := CanonicalRecord(v)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(canonical), nil
}

// VerifyRecordBinding recomputes HashRecord over recordJSON and compares it
// with r.ParamsSHA256. It does not verify r's signature or chain position.
func VerifyRecordBinding(r Receipt, recordJSON []byte) error {
	hash, err := HashRecord(json.RawMessage(recordJSON))
	if err != nil {
		return err
	}
	if hash != r.ParamsSHA256 {
		return ErrRecordMismatch
	}
	return nil
}

func canonicalValue(dec *json.Decoder, tok json.Token, depth int) ([]byte, error) {
	switch value := tok.(type) {
	case json.Delim:
		if depth >= maxRecordDepth {
			return nil, ErrInvalidRecord
		}
		switch value {
		case '{':
			return canonicalObject(dec, depth+1)
		case '[':
			return canonicalArray(dec, depth+1)
		}
		return nil, ErrInvalidRecord
	case string:
		return appendJCSString(nil, value), nil
	case json.Number:
		s := value.String()
		if !integerPattern.MatchString(s) || s == "-0" {
			return nil, ErrUnsupportedNumber
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n > maxSafeInteger || n < -maxSafeInteger {
			return nil, ErrUnsupportedNumber
		}
		return []byte(s), nil
	case bool:
		return strconv.AppendBool(nil, value), nil
	case nil:
		return []byte("null"), nil
	}
	return nil, ErrInvalidRecord
}

func canonicalObject(dec *json.Decoder, depth int) ([]byte, error) {
	type member struct {
		key   string
		value []byte
	}
	var members []member
	seen := make(map[string]struct{})
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, ErrInvalidRecord
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, ErrInvalidRecord
		}
		if _, dup := seen[key]; dup {
			return nil, ErrInvalidRecord
		}
		seen[key] = struct{}{}
		valTok, err := dec.Token()
		if err != nil {
			return nil, ErrInvalidRecord
		}
		value, err := canonicalValue(dec, valTok, depth)
		if err != nil {
			return nil, err
		}
		members = append(members, member{key, value})
	}
	if end, err := dec.Token(); err != nil || end != json.Delim('}') {
		return nil, ErrInvalidRecord
	}
	sort.Slice(members, func(i, j int) bool { return lessUTF16(members[i].key, members[j].key) })

	out := []byte{'{'}
	for i, m := range members {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendJCSString(out, m.key)
		out = append(out, ':')
		out = append(out, m.value...)
	}
	return append(out, '}'), nil
}

func canonicalArray(dec *json.Decoder, depth int) ([]byte, error) {
	out := []byte{'['}
	for first := true; dec.More(); first = false {
		tok, err := dec.Token()
		if err != nil {
			return nil, ErrInvalidRecord
		}
		value, err := canonicalValue(dec, tok, depth)
		if err != nil {
			return nil, err
		}
		if !first {
			out = append(out, ',')
		}
		out = append(out, value...)
	}
	if end, err := dec.Token(); err != nil || end != json.Delim(']') {
		return nil, ErrInvalidRecord
	}
	return append(out, ']'), nil
}

// lessUTF16 orders strings by UTF-16 code units, as RFC 8785 section 3.2.3 requires.
func lessUTF16(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

// appendJCSString escapes only '"', '\\', and control characters, using the
// short forms where JSON defines them and lowercase \u00xx otherwise.
func appendJCSString(dst []byte, s string) []byte {
	const hexDigits = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\t':
			dst = append(dst, '\\', 't')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\r':
			dst = append(dst, '\\', 'r')
		default:
			if c < 0x20 {
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			} else {
				dst = append(dst, c)
			}
		}
	}
	return append(dst, '"')
}

// validSurrogateEscapes rejects lone \uD800-\uDFFF escapes, which
// encoding/json would otherwise silently decode to U+FFFD.
func validSurrogateEscapes(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(raw) {
				continue
			}
			if raw[i+1] != 'u' {
				i++
				continue
			}
			first, ok := hexQuad(raw, i+2)
			if !ok {
				return false
			}
			switch {
			case first >= 0xdc00 && first <= 0xdfff:
				return false
			case first >= 0xd800 && first <= 0xdbff:
				if i+12 > len(raw) || raw[i+6] != '\\' || raw[i+7] != 'u' {
					return false
				}
				second, ok := hexQuad(raw, i+8)
				if !ok || second < 0xdc00 || second > 0xdfff {
					return false
				}
				i += 11
			default:
				i += 5
			}
		}
	}
	return true
}

func hexQuad(raw []byte, start int) (uint16, bool) {
	if start+4 > len(raw) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(raw[start:start+4]), 16, 16)
	return uint16(value), err == nil
}
