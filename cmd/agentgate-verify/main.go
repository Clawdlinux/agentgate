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
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	_ "modernc.org/sqlite"

	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
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
		outputFormat string
		quiet        bool
	)
	fs.StringVar(&source, "source", "", "receipt source: sqlite | jsonl")
	fs.StringVar(&path, "path", "", "input path; '-' means stdin (jsonl source only)")
	fs.StringVar(&trustRoot, "trust-root", "", "path to a JSON trust file; optional if the jsonl source embeds its own keys")
	fs.StringVar(&expectedHead, "expected-head", "", "optional SEQ:HEXHASH; overrides a manifest-derived expected head")
	fs.StringVar(&outputFormat, "format", "text", "output format: text | json")
	fs.BoolVar(&quiet, "quiet", false, "suppress successful verification details in text output")
	fs.BoolVar(&quiet, "q", false, "suppress successful verification details in text output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if outputFormat != "text" && outputFormat != "json" {
		fmt.Fprintf(stderr, "agentgate-verify: --format must be text or json, got %q\n", outputFormat)
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

	var explicitTrust []receiptspec.TrustedKey
	if trustRoot != "" {
		trustData, err := os.ReadFile(trustRoot)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: read trust root: %v\n", err)
			return 2
		}
		explicitTrust, err = receiptspec.LoadTrustedKeys(trustData)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
			return 2
		}
	}

	var explicitExpected *receiptspec.ExpectedHead
	if expectedHead != "" {
		eh, err := receiptspec.ParseExpectedHead(expectedHead)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
			return 2
		}
		explicitExpected = &eh
	}

	var (
		bundle receiptspec.Bundle
		err    error
	)
	switch source {
	case "sqlite":
		bundle.Receipts, err = readSQLite(path)
	case "jsonl":
		bundle, err = readJSONL(path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: read %s: %v\n", source, err)
		return 2
	}
	if len(explicitTrust) == 0 && len(bundle.Keys) == 0 {
		fmt.Fprintln(stderr, "agentgate-verify: --trust-root is required (the source has no embedded keys)")
		return 2
	}
	manifest := bundle.Manifest

	result, err := receiptspec.VerifyBundle(bundle, explicitTrust, explicitExpected)
	var manifestErr *receiptspec.ManifestError
	if errors.As(err, &manifestErr) {
		fmt.Fprintf(stderr, "agentgate-verify: manifest: %v\n", manifestErr.Err)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
		return 2
	}
	rangeKind := ""
	if manifest != nil {
		if manifest.ResolvedTo == manifest.HeadSeq {
			rangeKind = "full"
		} else {
			rangeKind = "partial"
		}
	}

	if result.OK {
		if outputFormat == "json" {
			writeJSONResult(stdout, result, rangeKind)
			return 0
		}
		fmt.Fprintf(stdout, "PASS: %d receipts verified, head seq=%d hash=%x\n",
			result.VerifiedCount, result.HeadSeq, result.HeadEntryHash[:8])
		if quiet {
			return 0
		}
		if result.Complete {
			fmt.Fprintln(stdout, "completeness: proven against the supplied expected head")
		} else {
			fmt.Fprintln(stdout, "completeness: not claimed (no --expected-head supplied)")
		}
		if manifest != nil {
			if rangeKind == "full" {
				fmt.Fprintln(stdout, "range: full (reaches the database's true head at export time)")
			} else {
				fmt.Fprintf(stdout, "range: partial (head at export time was seq=%d)\n", manifest.HeadSeq)
			}
		}
		return 0
	}
	if outputFormat == "json" {
		writeJSONResult(stdout, result, rangeKind)
		return 1
	}

	fmt.Fprintf(stderr, "FAIL: seq=%d reason=%s (%d of %d receipts verified before failure)\n",
		result.FailedAtSeq, result.Reason, result.VerifiedCount, result.TotalReceipts)
	return 1
}

type jsonResult struct {
	OK            bool   `json:"ok"`
	TotalReceipts int    `json:"total_receipts"`
	VerifiedCount int    `json:"verified_count"`
	FailedAtSeq   uint64 `json:"failed_at_seq,omitempty"`
	Reason        string `json:"reason,omitempty"`
	HeadSeq       uint64 `json:"head_seq,omitempty"`
	HeadEntryHash string `json:"head_entry_hash,omitempty"`
	Complete      bool   `json:"complete"`
	Range         string `json:"range,omitempty"`
}

func writeJSONResult(stdout io.Writer, result receiptspec.VerifyResult, rangeKind string) {
	output := jsonResult{
		OK:            result.OK,
		TotalReceipts: result.TotalReceipts,
		VerifiedCount: result.VerifiedCount,
		FailedAtSeq:   result.FailedAtSeq,
		Reason:        result.Reason,
		HeadSeq:       result.HeadSeq,
		Complete:      result.Complete,
		Range:         rangeKind,
	}
	if result.OK {
		output.HeadEntryHash = fmt.Sprintf("%x", result.HeadEntryHash)
	}
	_ = json.NewEncoder(stdout).Encode(output)
}

// readSQLite reads every row of the receipts table, ordered by seq, from a
// local file. It never writes to the database.
func readSQLite(path string) ([]receiptspec.Receipt, error) {
	db, err := sql.Open("sqlite", path)
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

	var out []receiptspec.Receipt
	for rows.Next() {
		r, err := receipt.ScanReceiptRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// readJSONL reads path (or stdin when path is "-") with receiptspec.ReadJSONL.
func readJSONL(path string) (receiptspec.Bundle, error) {
	if path == "-" {
		return receiptspec.ReadJSONL(os.Stdin)
	}
	f, err := os.Open(path)
	if err != nil {
		return receiptspec.Bundle{}, err
	}
	defer f.Close()
	return receiptspec.ReadJSONL(f)
}
