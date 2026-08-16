/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Clawdlinux/agentgate/internal/signer"
)

// ExportManifestDomainV1 separates export-manifest hashes from receipt
// entry hashes and every other protocol this package signs.
const ExportManifestDomainV1 = "agentgate.receipt.export-manifest.v1"

// ExportFormatVersion is the only manifest format_version this build
// produces or accepts.
const ExportFormatVersion = 1

// ErrManifestSignatureInvalid means a manifest's signature did not verify
// under its claimed signer_kid — a tamper finding, not a config error.
var ErrManifestSignatureInvalid = errors.New("receipt: export manifest signature invalid")

// ErrKeysetDigestMismatch means the embedded key lines do not match the
// manifest's own signed keyset_digest — a tamper finding.
var ErrKeysetDigestMismatch = errors.New("receipt: export manifest keyset digest mismatch")

// ExportManifest is the first line of a signed bounded export: it binds
// the requested and resolved range, the anchor immediately before the
// export, the first/last exported entry hashes, the database's true head
// at snapshot time, and a digest of every embedded key line — signed by
// the same Ed25519 signer that signs every receipt.
type ExportManifest struct {
	FormatVersion  int
	RequestedFrom  uint64
	RequestedTo    uint64 // 0 means the request left "to" unspecified
	ResolvedTo     uint64
	Count          int
	AnchorSeq      uint64
	AnchorHash     [32]byte
	FirstEntryHash [32]byte
	LastEntryHash  [32]byte
	HeadSeq        uint64
	HeadHash       [32]byte
	KeysetDigest   [32]byte
	SignerKID      string
	Signature      [64]byte
}

// ComputeKeysetDigest returns a deterministic digest over every trusted
// key, independent of input order — sorted by kid before hashing.
func ComputeKeysetDigest(keys []TrustedKey) [32]byte {
	sorted := make([]TrustedKey, len(keys))
	copy(sorted, keys)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].KID < sorted[j].KID })

	var buf []byte
	for _, k := range sorted {
		buf = appendLP(buf, k.KID)
		buf = append(buf, k.PublicKey...)
		buf = binary.LittleEndian.AppendUint64(buf, k.ValidFromSeq)
		if k.ValidUntilSeq != nil {
			buf = append(buf, 1)
			buf = binary.LittleEndian.AppendUint64(buf, *k.ValidUntilSeq)
		} else {
			buf = append(buf, 0)
		}
	}
	return sha256.Sum256(buf)
}

// canonicalManifestInput returns the manifest's signed preimage. It
// excludes SignerKID's signature (there is nothing to sign yet) but
// includes SignerKID itself, following the same pattern as
// CanonicalHashInput excluding EntryHash/Signature but including SignerKID.
func canonicalManifestInput(m ExportManifest) []byte {
	encoded := make([]byte, 0, 256)
	encoded = append(encoded, ExportManifestDomainV1...)
	encoded = append(encoded, 0)
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(m.FormatVersion))
	encoded = binary.LittleEndian.AppendUint64(encoded, m.RequestedFrom)
	encoded = binary.LittleEndian.AppendUint64(encoded, m.RequestedTo)
	encoded = binary.LittleEndian.AppendUint64(encoded, m.ResolvedTo)
	encoded = binary.LittleEndian.AppendUint64(encoded, uint64(m.Count))
	encoded = binary.LittleEndian.AppendUint64(encoded, m.AnchorSeq)
	encoded = append(encoded, m.AnchorHash[:]...)
	encoded = append(encoded, m.FirstEntryHash[:]...)
	encoded = append(encoded, m.LastEntryHash[:]...)
	encoded = binary.LittleEndian.AppendUint64(encoded, m.HeadSeq)
	encoded = append(encoded, m.HeadHash[:]...)
	encoded = append(encoded, m.KeysetDigest[:]...)
	encoded = appendLP(encoded, m.SignerKID)
	return encoded
}

// ComputeManifestHash returns the SHA-256 hash a manifest signs.
func ComputeManifestHash(m ExportManifest) [32]byte {
	return sha256.Sum256(canonicalManifestInput(m))
}

// SignManifest sets m's SignerKID and Signature, signing with priv.
func SignManifest(m ExportManifest, kid string, priv ed25519.PrivateKey) ExportManifest {
	m.SignerKID = kid
	hash := ComputeManifestHash(m)
	m.Signature = signer.Sign(priv, hash[:])
	return m
}

// VerifyManifest checks m's signature against trustedKeys and that its
// own KeysetDigest matches embeddedKeys — both tamper findings, not
// configuration errors, since the manifest itself is what carries trust
// here (VER-09's "unknown signer_kid is a config error" reasoning does not
// apply: a manifest naming an unrecognized key inside a signed export it
// otherwise controls is a forgery attempt, not a stale local trust file).
func VerifyManifest(m ExportManifest, trustedKeys []TrustedKey, embeddedKeys []TrustedKey) error {
	var key TrustedKey
	found := false
	for _, k := range trustedKeys {
		if k.KID == m.SignerKID {
			key, found = k, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrUnknownSignerKID, m.SignerKID)
	}
	hash := ComputeManifestHash(m)
	if !signer.Verify(key.PublicKey, hash[:], m.Signature) {
		return ErrManifestSignatureInvalid
	}
	if ComputeKeysetDigest(embeddedKeys) != m.KeysetDigest {
		return ErrKeysetDigestMismatch
	}
	return nil
}

// --- Typed JSONL lines: "manifest", "key", "receipt" ---

type lineTypeProbe struct {
	Type string `json:"type"`
}

// DetectJSONLLineType returns "manifest", "key", or "receipt" for one
// JSONL line. A missing or empty "type" field defaults to "receipt" —
// Phase 5's original plain-receipt-per-line format has no such field, and
// must keep parsing exactly as it always has.
func DetectJSONLLineType(line []byte) string {
	var probe lineTypeProbe
	if err := json.Unmarshal(line, &probe); err != nil || probe.Type == "" {
		return "receipt"
	}
	return probe.Type
}

type manifestLineJSON struct {
	Type           string `json:"type"`
	FormatVersion  int    `json:"format_version"`
	RequestedFrom  uint64 `json:"requested_from"`
	RequestedTo    uint64 `json:"requested_to,omitempty"`
	ResolvedTo     uint64 `json:"resolved_to"`
	Count          int    `json:"count"`
	AnchorSeq      uint64 `json:"anchor_seq"`
	AnchorHash     string `json:"anchor_hash"`
	FirstEntryHash string `json:"first_entry_hash"`
	LastEntryHash  string `json:"last_entry_hash"`
	HeadSeq        uint64 `json:"head_seq"`
	HeadHash       string `json:"head_hash"`
	KeysetDigest   string `json:"keyset_digest"`
	SignerKID      string `json:"signer_kid"`
	Signature      string `json:"signature"`
}

// MarshalManifestLine encodes m as one JSONL line.
func MarshalManifestLine(m ExportManifest) ([]byte, error) {
	line := manifestLineJSON{
		Type:           "manifest",
		FormatVersion:  m.FormatVersion,
		RequestedFrom:  m.RequestedFrom,
		RequestedTo:    m.RequestedTo,
		ResolvedTo:     m.ResolvedTo,
		Count:          m.Count,
		AnchorSeq:      m.AnchorSeq,
		AnchorHash:     hex.EncodeToString(m.AnchorHash[:]),
		FirstEntryHash: hex.EncodeToString(m.FirstEntryHash[:]),
		LastEntryHash:  hex.EncodeToString(m.LastEntryHash[:]),
		HeadSeq:        m.HeadSeq,
		HeadHash:       hex.EncodeToString(m.HeadHash[:]),
		KeysetDigest:   hex.EncodeToString(m.KeysetDigest[:]),
		SignerKID:      m.SignerKID,
		Signature:      hex.EncodeToString(m.Signature[:]),
	}
	return json.Marshal(line)
}

// ParseManifestLine decodes one JSONL manifest line, rejecting any
// format_version other than ExportFormatVersion.
func ParseManifestLine(line []byte) (ExportManifest, error) {
	var jl manifestLineJSON
	if err := json.Unmarshal(line, &jl); err != nil {
		return ExportManifest{}, fmt.Errorf("receipt: parse manifest line: %w", err)
	}
	if jl.FormatVersion != ExportFormatVersion {
		return ExportManifest{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedFormatVersion, jl.FormatVersion, ExportFormatVersion)
	}
	m := ExportManifest{
		FormatVersion: jl.FormatVersion,
		RequestedFrom: jl.RequestedFrom,
		RequestedTo:   jl.RequestedTo,
		ResolvedTo:    jl.ResolvedTo,
		Count:         jl.Count,
		AnchorSeq:     jl.AnchorSeq,
		HeadSeq:       jl.HeadSeq,
		SignerKID:     jl.SignerKID,
	}
	for _, f := range []struct {
		name string
		hex  string
		dst  []byte
	}{
		{"anchor_hash", jl.AnchorHash, m.AnchorHash[:]},
		{"first_entry_hash", jl.FirstEntryHash, m.FirstEntryHash[:]},
		{"last_entry_hash", jl.LastEntryHash, m.LastEntryHash[:]},
		{"head_hash", jl.HeadHash, m.HeadHash[:]},
		{"keyset_digest", jl.KeysetDigest, m.KeysetDigest[:]},
		{"signature", jl.Signature, m.Signature[:]},
	} {
		if err := decodeFixed(f.hex, f.dst); err != nil {
			return ExportManifest{}, fmt.Errorf("receipt: manifest %s: %w", f.name, err)
		}
	}
	return m, nil
}

type keyLineJSON struct {
	Type          string  `json:"type"`
	KID           string  `json:"kid"`
	PublicKeyHex  string  `json:"public_key_hex"`
	ValidFromSeq  uint64  `json:"valid_from_seq"`
	ValidUntilSeq *uint64 `json:"valid_until_seq"`
}

// MarshalKeyLine encodes k as one JSONL line.
func MarshalKeyLine(k TrustedKey) ([]byte, error) {
	return json.Marshal(keyLineJSON{
		Type:          "key",
		KID:           k.KID,
		PublicKeyHex:  hex.EncodeToString(k.PublicKey),
		ValidFromSeq:  k.ValidFromSeq,
		ValidUntilSeq: k.ValidUntilSeq,
	})
}

// ParseKeyLine decodes one JSONL key line into a TrustedKey.
func ParseKeyLine(line []byte) (TrustedKey, error) {
	var jl keyLineJSON
	if err := json.Unmarshal(line, &jl); err != nil {
		return TrustedKey{}, fmt.Errorf("receipt: parse key line: %w", err)
	}
	pub, err := hex.DecodeString(jl.PublicKeyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return TrustedKey{}, fmt.Errorf("receipt: key line %s: public_key_hex must be %d bytes hex", jl.KID, ed25519.PublicKeySize)
	}
	return TrustedKey{
		KID:           jl.KID,
		PublicKey:     ed25519.PublicKey(pub),
		ValidFromSeq:  jl.ValidFromSeq,
		ValidUntilSeq: jl.ValidUntilSeq,
	}, nil
}
