/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Command gen writes deterministic v1 receipt fixtures. Keys are test-only.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

type outcome struct {
	Class         string `json:"class"`
	VerifiedCount int    `json:"verified_count"`
	FailedAtSeq   uint64 `json:"failed_at_seq"`
	Complete      bool   `json:"complete"`
}

type vector struct {
	Name     string  `json:"name"`
	JSONL    string  `json:"jsonl"`
	Keys     string  `json:"keys"`
	Expected outcome `json:"expected"`
}

type catalog struct {
	FormatVersion int      `json:"format_version"`
	Vectors       []vector `json:"vectors"`
}

func main() {
	out := flag.String("out", "", "output directory (required)")
	flag.Parse()
	if *out == "" {
		fatal(fmt.Errorf("-out is required"))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	generate(*out)
}

func generate(out string) {
	var seedA, seedB [ed25519.SeedSize]byte
	for i := range seedA {
		seedA[i], seedB[i] = byte(i), byte(i+32)
	}
	privA := ed25519.NewKeyFromSeed(seedA[:])
	privB := ed25519.NewKeyFromSeed(seedB[:])
	signerA := receiptspec.NewEd25519Signer(privA)
	signerB := receiptspec.NewEd25519Signer(privB)
	until := uint64(2)
	keys := []receiptspec.TrustedKey{
		{KID: signerA.KID(), PublicKey: privA.Public().(ed25519.PublicKey), ValidFromSeq: 1, ValidUntilSeq: &until},
		{KID: signerB.KID(), PublicKey: privB.Public().(ed25519.PublicKey), ValidFromSeq: 3},
	}
	writeTrust(out, "public-keys.json", keys)

	base := receiptspec.Fields{
		TimestampUnixNS: 1, HumanPrincipal: "h", AgentKeyID: "a",
		Service: "s", Action: "x", ParamsSHA256: sha256.Sum256([]byte("{}")),
		PolicyDecision: receiptspec.DecisionAllow, StatusCode: 100,
	}
	chainA := receiptspec.NewChain(signerA, 0, [32]byte{})
	first, err := chainA.Next(base)
	check(err)
	secondFields := base
	secondFields.TimestampUnixNS = 1_700_000_000_000_000_001
	secondFields.HumanPrincipal = "human-é/e\u0301/😀"
	secondFields.AgentKeyID = "agent-2"
	secondFields.Service, secondFields.Action = "operator", "decision:scale"
	secondFields.PolicyDecision, secondFields.StatusCode = receiptspec.DecisionDeny, 403
	secondFields.Error, secondFields.LatencyMS = "scope_denied", 17
	for _, code := range []string{`check if true`, `check if "github" == "github"`} {
		digest := sha256.Sum256([]byte(code))
		secondFields.DelegationChain = append(secondFields.DelegationChain, hex.EncodeToString(digest[:]))
	}
	secondFields.ParamsSHA256, err = receiptspec.HashRecord(json.RawMessage(`{"replicas":3,"note":"é/e\u0301/😀","confidence":"0.875"}`))
	check(err)
	second, err := chainA.Next(secondFields)
	check(err)
	chainB := receiptspec.NewChain(signerB, second.Seq, second.EntryHash)
	thirdFields := base
	thirdFields.TimestampUnixNS = ^uint64(0)
	thirdFields.LatencyMS = int64(^uint64(0) >> 1)
	thirdFields.StatusCode, thirdFields.PolicyDecision = 599, receiptspec.DecisionRateLimited
	thirdFields.Error = strings.Repeat("e", 64)
	third, err := chainB.Next(thirdFields)
	check(err)
	receipts := []receiptspec.Receipt{first, second, third}

	manifest := func(rs []receiptspec.Receipt, anchor receiptspec.Anchor, head receiptspec.Receipt) *receiptspec.ExportManifest {
		m := receiptspec.ExportManifest{
			FormatVersion: 1, RequestedFrom: rs[0].Seq, ResolvedTo: rs[len(rs)-1].Seq,
			Count: len(rs), AnchorSeq: anchor.Seq, AnchorHash: anchor.EntryHash,
			FirstEntryHash: rs[0].EntryHash, LastEntryHash: rs[len(rs)-1].EntryHash,
			HeadSeq: head.Seq, HeadHash: head.EntryHash, KeysetDigest: receiptspec.ComputeKeysetDigest(keys),
		}
		m = receiptspec.SignManifest(m, signerB.KID(), privB)
		return &m
	}
	full := manifest(receipts, receiptspec.Anchor{}, third)
	c := catalog{FormatVersion: 1}
	add := func(name string, rs []receiptspec.Receipt, embedded []receiptspec.TrustedKey, m *receiptspec.ExportManifest, trust string, want outcome) {
		var buf bytes.Buffer
		if m != nil {
			line, err := receiptspec.MarshalManifestLine(*m)
			check(err)
			buf.Write(line)
			buf.WriteByte('\n')
		}
		for _, key := range embedded {
			line, err := receiptspec.MarshalKeyLine(key)
			check(err)
			buf.Write(line)
			buf.WriteByte('\n')
		}
		check(receiptspec.WriteJSONL(&buf, rs))
		write(out, name+".jsonl", buf.Bytes())
		c.Vectors = append(c.Vectors, vector{Name: name, JSONL: name + ".jsonl", Keys: trust, Expected: want})
	}
	addCommon := func(name string, rs []receiptspec.Receipt, m *receiptspec.ExportManifest, want outcome) {
		add(name, rs, keys, m, "public-keys.json", want)
	}
	addCommon("valid-genesis", receipts[:1], nil, outcome{Class: "ok", VerifiedCount: 1})
	addCommon("valid-chain", receipts, full, outcome{Class: "ok", VerifiedCount: 3, Complete: true})
	addCommon("valid-partial", receipts[1:], manifest(receipts[1:], receiptspec.Anchor{Seq: 1, EntryHash: first.EntryHash}, third),
		outcome{Class: "ok", VerifiedCount: 2, Complete: true})
	addCommon("valid-unpinned-prefix", receipts[:2], nil, outcome{Class: "ok", VerifiedCount: 2})
	addCommon("valid-rotation-straddle", receipts, full, outcome{Class: "ok", VerifiedCount: 3, Complete: true})
	addCommon("valid-rotation-before-first-receipt", receipts[:2], manifest(receipts[:2], receiptspec.Anchor{}, second),
		outcome{Class: "ok", VerifiedCount: 2, Complete: true})
	historical := manifest(receipts[:1], receiptspec.Anchor{}, third)
	addCommon("valid-historical-range", receipts[:1], historical, outcome{Class: "ok", VerifiedCount: 1, Complete: true})
	retiredBoundary := receiptspec.SignManifest(*manifest(receipts[:2], receiptspec.Anchor{}, second), signerA.KID(), privA)
	addCommon("valid-retired-at-boundary", receipts[:2], &retiredBoundary, outcome{Class: "ok", VerifiedCount: 2, Complete: true})
	empty := receiptspec.ExportManifest{
		FormatVersion: 1, RequestedFrom: 1, KeysetDigest: receiptspec.ComputeKeysetDigest(keys),
	}
	empty = receiptspec.SignManifest(empty, signerA.KID(), privA)
	addCommon("valid-empty-genesis", nil, &empty, outcome{Class: "ok", Complete: true})
	empty.RequestedFrom, empty.ResolvedTo, empty.AnchorSeq, empty.HeadSeq = 4, 3, 3, 3
	empty.AnchorHash, empty.FirstEntryHash, empty.LastEntryHash, empty.HeadHash = third.EntryHash, third.EntryHash, third.EntryHash, third.EntryHash
	empty = receiptspec.SignManifest(empty, signerB.KID(), privB)
	addCommon("valid-empty-anchor", nil, &empty, outcome{Class: "ok", Complete: true})

	maxFields := thirdFields
	maxFields.HumanPrincipal = strings.Repeat("é", 128)
	maxFields.AgentKeyID, maxFields.Service, maxFields.Action = strings.Repeat("a", 128), strings.Repeat("s", 64), strings.Repeat("x", 128)
	maxFields.DelegationChain = make([]string, 32)
	for i := range maxFields.DelegationChain {
		digest := sha256.Sum256([]byte(fmt.Sprintf("block-%d", i)))
		maxFields.DelegationChain[i] = hex.EncodeToString(digest[:])
	}
	maxAnchor := receiptspec.Anchor{Seq: ^uint64(0) - 1, EntryHash: sha256.Sum256([]byte("boundary anchor"))}
	maxChain := receiptspec.NewChain(signerB, maxAnchor.Seq, maxAnchor.EntryHash)
	maxReceipt, err := maxChain.Next(maxFields)
	check(err)
	maxReceipts := []receiptspec.Receipt{maxReceipt}
	addCommon("valid-boundary", maxReceipts, manifest(maxReceipts, maxAnchor, maxReceipt),
		outcome{Class: "ok", VerifiedCount: 1, Complete: true})

	modified := func(change func(*receiptspec.Receipt)) []receiptspec.Receipt {
		copyOfReceipts := append([]receiptspec.Receipt(nil), receipts...)
		change(&copyOfReceipts[1])
		return copyOfReceipts
	}
	addCommon("tampered-body", modified(func(r *receiptspec.Receipt) { r.Action = "decision:delete" }), full,
		outcome{Class: receiptspec.ReasonEntryHashMismatch, VerifiedCount: 1, FailedAtSeq: 2})
	addCommon("reordered-chain", []receiptspec.Receipt{second, first, third}, full,
		outcome{Class: receiptspec.ReasonSequenceGap, FailedAtSeq: 2})
	addCommon("missing-middle", []receiptspec.Receipt{first, third}, full,
		outcome{Class: receiptspec.ReasonSequenceGap, VerifiedCount: 1, FailedAtSeq: 3})
	addCommon("missing-tail-with-manifest", receipts[:2], full,
		outcome{Class: receiptspec.ReasonExpectedHeadMismatch, VerifiedCount: 2, FailedAtSeq: 3})
	addCommon("wrong-key-signature", modified(func(r *receiptspec.Receipt) {
		copy(r.Signature[:], ed25519.Sign(privB, r.EntryHash[:]))
	}), full, outcome{Class: receiptspec.ReasonSignatureInvalid, VerifiedCount: 1, FailedAtSeq: 2})
	addCommon("bad-prev-hash", modified(func(r *receiptspec.Receipt) { r.PrevHash[0] ^= 1 }), full,
		outcome{Class: receiptspec.ReasonPrevHashMismatch, VerifiedCount: 1, FailedAtSeq: 2})
	addCommon("invalid-fields", modified(func(r *receiptspec.Receipt) { r.StatusCode = 99 }), full,
		outcome{Class: receiptspec.ReasonInvalidReceiptFields, VerifiedCount: 1, FailedAtSeq: 2})
	addCommon("unknown-signer", modified(func(r *receiptspec.Receipt) { r.SignerKID = "unknown" }), full,
		outcome{Class: "ErrUnknownSignerKID"})
	addCommon("empty-chain", nil, nil, outcome{Class: "ErrEmptyChain"})

	inactive := append([]receiptspec.TrustedKey(nil), keys...)
	inactive[0].ValidFromSeq = 2
	writeTrust(out, "inactive-keys.json", inactive)
	add("inactive-signer", receipts[:1], inactive, nil, "inactive-keys.json",
		outcome{Class: receiptspec.ReasonSignerInactiveAtSeq, FailedAtSeq: 1})

	badManifest := *full
	badManifest.HeadHash[0] ^= 1
	addCommon("manifest-mismatch", receipts, &badManifest, outcome{Class: "ManifestError/ErrManifestSignatureInvalid"})
	add("manifest-keyset-mismatch", receipts, keys[1:], full, "public-keys.json",
		outcome{Class: "ManifestError/ErrKeysetDigestMismatch"})
	badManifest = *full
	badManifest.SignerKID = "unknown"
	addCommon("manifest-unknown-signer", receipts, &badManifest, outcome{Class: "ManifestError/ErrUnknownSignerKID"})

	badManifest = *full
	badManifest.ResolvedTo++
	badManifest = receiptspec.SignManifest(badManifest, signerB.KID(), privB)
	addCommon("manifest-endpoint-mismatch", receipts, &badManifest,
		outcome{Class: "manifest_resolved_to_beyond_head", FailedAtSeq: 4})
	badManifest = *full
	badManifest.Count = 99
	badManifest = receiptspec.SignManifest(badManifest, signerB.KID(), privB)
	addCommon("manifest-inflated-count", receipts, &badManifest,
		outcome{Class: "manifest_count_mismatch", FailedAtSeq: 3})

	retired := receiptspec.SignManifest(*full, signerA.KID(), privA)
	addCommon("manifest-retired-signer-full-range", receipts, &retired,
		outcome{Class: "manifest_signer_inactive_at_head", FailedAtSeq: 3})
	tooEarly := *historical
	tooEarly.HeadSeq, tooEarly.HeadHash = first.Seq, first.EntryHash
	tooEarly = receiptspec.SignManifest(tooEarly, signerB.KID(), privB)
	addCommon("manifest-signer-before-window", receipts[:1], &tooEarly,
		outcome{Class: "manifest_signer_inactive_at_head", FailedAtSeq: 1})
	addBadManifest := func(name string, change func(*receiptspec.ExportManifest), want outcome) {
		m := *full
		change(&m)
		m = receiptspec.SignManifest(m, signerB.KID(), privB)
		addCommon(name, receipts, &m, want)
	}
	addBadManifest("manifest-count-mismatch", func(m *receiptspec.ExportManifest) { m.Count-- },
		outcome{Class: "manifest_count_mismatch", FailedAtSeq: 3})
	addBadManifest("manifest-negative-count", func(m *receiptspec.ExportManifest) {
		m.Count, m.ResolvedTo, m.HeadSeq = -1, ^uint64(0), ^uint64(0)
	}, outcome{Class: "manifest_count_mismatch", FailedAtSeq: ^uint64(0)})
	addBadManifest("manifest-reversed-range", func(m *receiptspec.ExportManifest) {
		m.AnchorSeq, m.ResolvedTo, m.Count = ^uint64(0), 0, 1
	}, outcome{Class: "manifest_count_mismatch"})
	addBadManifest("manifest-first-entry-hash-mismatch", func(m *receiptspec.ExportManifest) { m.FirstEntryHash[0] ^= 1 },
		outcome{Class: "manifest_first_entry_hash_mismatch", VerifiedCount: 3, FailedAtSeq: 1})
	addBadManifest("manifest-head-hash-mismatch", func(m *receiptspec.ExportManifest) { m.HeadHash[0] ^= 1 },
		outcome{Class: "manifest_head_hash_mismatch", FailedAtSeq: 3})
	badEmpty := empty
	badEmpty.HeadSeq++
	badEmpty.LastEntryHash[0] ^= 1
	badEmpty = receiptspec.SignManifest(badEmpty, signerB.KID(), privB)
	addCommon("manifest-empty-endpoint-mismatch", nil, &badEmpty,
		outcome{Class: receiptspec.ReasonExpectedHeadMismatch, FailedAtSeq: 3})

	preimage, err := receiptspec.CanonicalHashInput(first)
	check(err)
	writeJSON(out, "example.json", struct {
		SeedHex       string `json:"seed_hex"`
		PublicKeyHex  string `json:"public_key_hex"`
		KID           string `json:"kid"`
		PreimageHex   string `json:"preimage_hex"`
		PreimageBytes int    `json:"preimage_bytes"`
		EntryHash     string `json:"entry_hash"`
		Signature     string `json:"signature"`
	}{
		hex.EncodeToString(seedA[:]), hex.EncodeToString(keys[0].PublicKey), signerA.KID(),
		hex.EncodeToString(preimage), len(preimage), hex.EncodeToString(first.EntryHash[:]), hex.EncodeToString(first.Signature[:]),
	})
	writeJSON(out, "vectors.json", c)
}

func writeTrust(out, name string, keys []receiptspec.TrustedKey) {
	var lines []json.RawMessage
	for _, key := range keys {
		line, err := receiptspec.MarshalKeyLine(key)
		check(err)
		var fields map[string]json.RawMessage
		check(json.Unmarshal(line, &fields))
		delete(fields, "type")
		line, err = json.Marshal(fields)
		check(err)
		lines = append(lines, line)
	}
	writeJSON(out, name, struct {
		Keys []json.RawMessage `json:"keys"`
	}{lines})
}

func writeJSON(out, name string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	check(err)
	write(out, name, append(data, '\n'))
}

func write(out, name string, contents []byte) {
	check(os.WriteFile(filepath.Join(out, name), contents, 0o644))
}

func check(err error) {
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
