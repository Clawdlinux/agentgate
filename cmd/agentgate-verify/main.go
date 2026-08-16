/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Command agentgate-verify verifies an AgentGate receipt chain offline: no
// private key, signer process, gateway state, or network connection is
// required (VER-02). Two sources are supported:
//
//	--source sqlite  Reads the receipts table from a local SQLite file.
//	--source jsonl    Reads newline-delimited receipt JSON from --path
//	                  (or stdin with --path -). A signed bounded export
//	                  (GET /v1/receipts/export) embeds its own trusted
//	                  keys and manifest as typed lines; --trust-root is
//	                  then optional and, if omitted, the export's own
//	                  embedded keys become the trust set.
//
// --trust-root points to a JSON file of trusted signer keys (the same
// shape GET /v1/receipts/pubkey serves), saved once through a trusted
// channel; required unless the jsonl source embeds its own keys.
// --expected-head SEQ:HEXHASH overrides any manifest-derived expected head
// and additionally proves the checked range is complete, not merely
// internally consistent.
//
// Exit codes: 0 = all requested checks passed, 1 = chain, key, manifest,
// or signature mismatch, 2 = I/O, syntax, or configuration error.
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Clawdlinux/agentgate/internal/receipt"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agentgate-verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		source       string
		path         string
		trustRoot    string
		expectedHead string
	)
	fs.StringVar(&source, "source", "", "receipt source: sqlite | jsonl")
	fs.StringVar(&path, "path", "", "input path; '-' means stdin (jsonl source only)")
	fs.StringVar(&trustRoot, "trust-root", "", "path to a JSON trust file; optional if the jsonl source embeds its own keys")
	fs.StringVar(&expectedHead, "expected-head", "", "optional SEQ:HEXHASH; overrides a manifest-derived expected head")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if source != "sqlite" && source != "jsonl" {
		fmt.Fprintf(stderr, "agentgate-verify: --source must be sqlite or jsonl, got %q\n", source)
		return 2
	}
	if path == "" {
		fmt.Fprintln(stderr, "agentgate-verify: --path is required")
		return 2
	}

	var explicitTrust []receipt.TrustedKey
	if trustRoot != "" {
		trustData, err := os.ReadFile(trustRoot)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: read trust root: %v\n", err)
			return 2
		}
		explicitTrust, err = receipt.LoadTrustedKeys(trustData)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
			return 2
		}
	}

	var explicitExpected *receipt.ExpectedHead
	if expectedHead != "" {
		eh, err := receipt.ParseExpectedHead(expectedHead)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
			return 2
		}
		explicitExpected = &eh
	}

	var (
		receipts     []receipt.Receipt
		embeddedKeys []receipt.TrustedKey
		manifest     *receipt.ExportManifest
		err          error
	)
	switch source {
	case "sqlite":
		receipts, err = readSQLite(path)
	case "jsonl":
		receipts, embeddedKeys, manifest, err = readJSONL(path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: read %s: %v\n", source, err)
		return 2
	}

	trustedKeys := explicitTrust
	if len(trustedKeys) == 0 {
		if len(embeddedKeys) == 0 {
			fmt.Fprintln(stderr, "agentgate-verify: --trust-root is required (the source has no embedded keys)")
			return 2
		}
		trustedKeys = embeddedKeys
	}

	anchor := receipt.Anchor{}
	expected := explicitExpected
	if manifest != nil {
		if err := receipt.VerifyManifest(*manifest, trustedKeys, embeddedKeys); err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: manifest: %v\n", err)
			return 1
		}
		anchor = receipt.Anchor{Seq: manifest.AnchorSeq, EntryHash: manifest.AnchorHash}
		if expected == nil {
			expected = &receipt.ExpectedHead{Seq: manifest.ResolvedTo, EntryHash: manifest.LastEntryHash}
		}
	}

	result, err := receipt.VerifyChain(receipts, trustedKeys, anchor, expected)
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
		return 2
	}

	if result.OK {
		fmt.Fprintf(stdout, "PASS: %d receipts verified, head seq=%d hash=%x\n",
			result.VerifiedCount, result.HeadSeq, result.HeadEntryHash[:8])
		if result.Complete {
			fmt.Fprintln(stdout, "completeness: proven against the supplied expected head")
		} else {
			fmt.Fprintln(stdout, "completeness: not claimed (no --expected-head supplied)")
		}
		if manifest != nil {
			if manifest.ResolvedTo == manifest.HeadSeq {
				fmt.Fprintln(stdout, "range: full (reaches the database's true head at export time)")
			} else {
				fmt.Fprintf(stdout, "range: partial (head at export time was seq=%d)\n", manifest.HeadSeq)
			}
		}
		return 0
	}

	fmt.Fprintf(stderr, "FAIL: seq=%d reason=%s (%d of %d receipts verified before failure)\n",
		result.FailedAtSeq, result.Reason, result.VerifiedCount, result.TotalReceipts)
	return 1
}

// readSQLite reads every row of the receipts table, ordered by seq, from a
// local file. It never writes to the database.
func readSQLite(path string) ([]receipt.Receipt, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT seq, format_version, timestamp_unix_ns, human_principal, agent_key_id,
		       delegation_chain_json, service, action, params_sha256, policy_decision,
		       status_code, latency_ms, error_code, prev_hash, entry_hash, signer_kid, signature
		FROM receipts ORDER BY seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []receipt.Receipt
	for rows.Next() {
		r, err := receipt.ScanReceiptRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// readJSONL reads path (or stdin when path is "-"), dispatching each
// non-empty line by its "type" field: "key" lines accumulate into
// embeddedKeys, a "manifest" line becomes manifest (at most one is
// expected; a later one overwrites, since a well-formed export has
// exactly one), and everything else ("receipt", or no "type" field at
// all — Phase 5's original plain format) accumulates into receipts.
func readJSONL(path string) (receipts []receipt.Receipt, embeddedKeys []receipt.TrustedKey, manifest *receipt.ExportManifest, err error) {
	var rdr io.Reader
	if path == "-" {
		rdr = os.Stdin
	} else {
		f, ferr := os.Open(path)
		if ferr != nil {
			return nil, nil, nil, ferr
		}
		defer f.Close()
		rdr = f
	}

	scanner := bufio.NewScanner(rdr)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		switch receipt.DetectJSONLLineType(line) {
		case "manifest":
			m, perr := receipt.ParseManifestLine(line)
			if perr != nil {
				return nil, nil, nil, fmt.Errorf("line %d: %w", lineNum, perr)
			}
			manifest = &m
		case "key":
			k, perr := receipt.ParseKeyLine(line)
			if perr != nil {
				return nil, nil, nil, fmt.Errorf("line %d: %w", lineNum, perr)
			}
			embeddedKeys = append(embeddedKeys, k)
		default:
			r, perr := receipt.ParseJSONLReceipt(line)
			if perr != nil {
				return nil, nil, nil, fmt.Errorf("line %d: %w", lineNum, perr)
			}
			receipts = append(receipts, r)
		}
	}
	if serr := scanner.Err(); serr != nil {
		return nil, nil, nil, serr
	}
	return receipts, embeddedKeys, manifest, nil
}
