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
//	                  (or stdin with --path -).
//
// --trust-root points to a JSON file of trusted signer keys (the same
// shape GET /v1/receipts/pubkey serves), saved once through a trusted
// channel. --expected-head SEQ:HEXHASH additionally proves the checked
// range is complete, not merely internally consistent.
//
// Exit codes: 0 = all requested checks passed, 1 = chain, key, or
// signature mismatch, 2 = I/O, syntax, or configuration error.
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
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
	fs.StringVar(&trustRoot, "trust-root", "", "path to a JSON trust file (array of trusted signer keys)")
	fs.StringVar(&expectedHead, "expected-head", "", "optional SEQ:HEXHASH to additionally prove completeness")
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
	if trustRoot == "" {
		fmt.Fprintln(stderr, "agentgate-verify: --trust-root is required")
		return 2
	}

	trustData, err := os.ReadFile(trustRoot)
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: read trust root: %v\n", err)
		return 2
	}
	trustedKeys, err := receipt.LoadTrustedKeys(trustData)
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
		return 2
	}

	var expected *receipt.ExpectedHead
	if expectedHead != "" {
		eh, err := receipt.ParseExpectedHead(expectedHead)
		if err != nil {
			fmt.Fprintf(stderr, "agentgate-verify: %v\n", err)
			return 2
		}
		expected = &eh
	}

	var receipts []receipt.Receipt
	switch source {
	case "sqlite":
		receipts, err = readSQLite(path)
	case "jsonl":
		receipts, err = readJSONL(path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "agentgate-verify: read %s: %v\n", source, err)
		return 2
	}

	result, err := receipt.VerifyChain(receipts, trustedKeys, expected)
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
		var r receipt.Receipt
		var formatVersion int
		var delegationJSON string
		var paramsSHA256, prevHash, entryHash, signature []byte

		if err := rows.Scan(&r.Seq, &formatVersion, &r.TimestampUnixNS, &r.HumanPrincipal, &r.AgentKeyID,
			&delegationJSON, &r.Service, &r.Action, &paramsSHA256, &r.PolicyDecision,
			&r.StatusCode, &r.LatencyMS, &r.Error, &prevHash, &entryHash, &r.SignerKID, &signature); err != nil {
			return nil, err
		}
		if formatVersion != receipt.JSONLFormatVersion {
			return nil, fmt.Errorf("%w: seq %d has format_version %d", receipt.ErrUnsupportedFormatVersion, r.Seq, formatVersion)
		}
		if err := json.Unmarshal([]byte(delegationJSON), &r.DelegationChain); err != nil {
			return nil, fmt.Errorf("seq %d: delegation_chain_json: %w", r.Seq, err)
		}
		for _, f := range []struct {
			name string
			src  []byte
			dst  []byte
		}{
			{"params_sha256", paramsSHA256, r.ParamsSHA256[:]},
			{"prev_hash", prevHash, r.PrevHash[:]},
			{"entry_hash", entryHash, r.EntryHash[:]},
			{"signature", signature, r.Signature[:]},
		} {
			if len(f.src) != len(f.dst) {
				return nil, fmt.Errorf("seq %d: %s: want %d bytes, got %d", r.Seq, f.name, len(f.dst), len(f.src))
			}
			copy(f.dst, f.src)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// readJSONL reads one receipt per non-empty line from path, or stdin when
// path is "-".
func readJSONL(path string) ([]receipt.Receipt, error) {
	var rdr io.Reader
	if path == "-" {
		rdr = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rdr = f
	}

	scanner := bufio.NewScanner(rdr)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	var out []receipt.Receipt
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		r, err := receipt.ParseJSONLReceipt(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", len(out)+1, err)
		}
		out = append(out, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
