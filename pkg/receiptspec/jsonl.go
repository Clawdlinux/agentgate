/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receiptspec

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// JSONLFormatVersion is the only format_version the JSONL reader and writer accept.
const JSONLFormatVersion = 1

// ErrUnsupportedFormatVersion is returned when a JSONL line or stored row
// declares a format_version this build does not understand.
var ErrUnsupportedFormatVersion = errors.New("receipt: unsupported format_version")

// jsonlReceiptV1 is the JSONL encoding of one Receipt. Fixed-length binary
// fields are lowercase hex. The canonical hash never depends on this encoding.
type jsonlReceiptV1 struct {
	Type            string   `json:"type,omitempty"`
	FormatVersion   int      `json:"format_version"`
	Seq             uint64   `json:"seq"`
	TimestampUnixNS uint64   `json:"timestamp_unix_ns"`
	HumanPrincipal  string   `json:"human_principal"`
	AgentKeyID      string   `json:"agent_key_id"`
	DelegationChain []string `json:"delegation_chain"`
	Service         string   `json:"service"`
	Action          string   `json:"action"`
	ParamsSHA256    string   `json:"params_sha256"`
	PolicyDecision  string   `json:"policy_decision"`
	StatusCode      int      `json:"status_code"`
	LatencyMS       int64    `json:"latency_ms"`
	Error           string   `json:"error"`
	PrevHash        string   `json:"prev_hash"`
	EntryHash       string   `json:"entry_hash"`
	SignerKID       string   `json:"signer_kid"`
	Signature       string   `json:"signature"`
}

// MarshalJSONLReceipt encodes r as one JSONL line (no trailing newline).
func MarshalJSONLReceipt(r Receipt) ([]byte, error) {
	line := jsonlReceiptV1{
		Type:            "receipt",
		FormatVersion:   JSONLFormatVersion,
		Seq:             r.Seq,
		TimestampUnixNS: r.TimestampUnixNS,
		HumanPrincipal:  r.HumanPrincipal,
		AgentKeyID:      r.AgentKeyID,
		DelegationChain: r.DelegationChain,
		Service:         r.Service,
		Action:          r.Action,
		ParamsSHA256:    hex.EncodeToString(r.ParamsSHA256[:]),
		PolicyDecision:  r.PolicyDecision,
		StatusCode:      r.StatusCode,
		LatencyMS:       r.LatencyMS,
		Error:           r.Error,
		PrevHash:        hex.EncodeToString(r.PrevHash[:]),
		EntryHash:       hex.EncodeToString(r.EntryHash[:]),
		SignerKID:       r.SignerKID,
		Signature:       hex.EncodeToString(r.Signature[:]),
	}
	return json.Marshal(line)
}

// ParseJSONLReceipt decodes one JSONL line into a Receipt. It rejects any
// format_version other than JSONLFormatVersion and any fixed-length field
// whose decoded byte length does not match the protocol.
func ParseJSONLReceipt(line []byte) (Receipt, error) {
	var jl jsonlReceiptV1
	if err := json.Unmarshal(line, &jl); err != nil {
		return Receipt{}, fmt.Errorf("receipt: parse jsonl line: %w", err)
	}
	if jl.FormatVersion != JSONLFormatVersion {
		return Receipt{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedFormatVersion, jl.FormatVersion, JSONLFormatVersion)
	}

	r := Receipt{
		Seq:             jl.Seq,
		TimestampUnixNS: jl.TimestampUnixNS,
		HumanPrincipal:  jl.HumanPrincipal,
		AgentKeyID:      jl.AgentKeyID,
		DelegationChain: jl.DelegationChain,
		Service:         jl.Service,
		Action:          jl.Action,
		PolicyDecision:  jl.PolicyDecision,
		StatusCode:      jl.StatusCode,
		LatencyMS:       jl.LatencyMS,
		Error:           jl.Error,
		SignerKID:       jl.SignerKID,
	}

	for _, f := range []struct {
		name string
		hex  string
		dst  []byte
	}{
		{"params_sha256", jl.ParamsSHA256, r.ParamsSHA256[:]},
		{"prev_hash", jl.PrevHash, r.PrevHash[:]},
		{"entry_hash", jl.EntryHash, r.EntryHash[:]},
		{"signature", jl.Signature, r.Signature[:]},
	} {
		if err := decodeFixed(f.hex, f.dst); err != nil {
			return Receipt{}, fmt.Errorf("receipt: %s: %w", f.name, err)
		}
	}
	return r, nil
}

func decodeFixed(hexStr string, dst []byte) error {
	decoded, err := hex.DecodeString(hexStr)
	if err != nil {
		return err
	}
	if len(decoded) != len(dst) {
		return fmt.Errorf("want %d bytes, got %d", len(dst), len(decoded))
	}
	copy(dst, decoded)
	return nil
}

type lineTypeProbe struct {
	Type string `json:"type"`
}

// DetectJSONLLineType returns "manifest", "key", or "receipt" for one JSONL
// line. A missing or empty "type" field defaults to "receipt", so the
// original plain receipt-per-line format keeps parsing.
func DetectJSONLLineType(line []byte) string {
	var probe lineTypeProbe
	if err := json.Unmarshal(line, &probe); err != nil || probe.Type == "" {
		return "receipt"
	}
	return probe.Type
}

// Bundle is the parsed content of one JSONL source: receipts in file order,
// any embedded "key" lines, and the "manifest" line if present.
type Bundle struct {
	Receipts []Receipt
	Keys     []TrustedKey
	Manifest *ExportManifest
}

// ReadJSONL parses every non-empty line of r. "key" lines go to Keys, a
// "manifest" line becomes Manifest (a later one overwrites an earlier one),
// and every other line is parsed as a receipt.
func ReadJSONL(r io.Reader) (Bundle, error) {
	var b Bundle
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		switch DetectJSONLLineType(line) {
		case "manifest":
			m, err := ParseManifestLine(line)
			if err != nil {
				return Bundle{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
			b.Manifest = &m
		case "key":
			k, err := ParseKeyLine(line)
			if err != nil {
				return Bundle{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
			b.Keys = append(b.Keys, k)
		default:
			rc, err := ParseJSONLReceipt(line)
			if err != nil {
				return Bundle{}, fmt.Errorf("line %d: %w", lineNum, err)
			}
			b.Receipts = append(b.Receipts, rc)
		}
	}
	if err := scanner.Err(); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

// WriteJSONL writes receipts to w, one MarshalJSONLReceipt line each,
// newline-terminated.
func WriteJSONL(w io.Writer, receipts []Receipt) error {
	for _, r := range receipts {
		line, err := MarshalJSONLReceipt(r)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}
