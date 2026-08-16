/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package main

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/signer"
)

type trustedKeyJSON struct {
	KID           string  `json:"kid"`
	PublicKeyHex  string  `json:"public_key_hex"`
	ValidFromSeq  uint64  `json:"valid_from_seq"`
	ValidUntilSeq *uint64 `json:"valid_until_seq"`
}

type trustFileJSON struct {
	Keys []trustedKeyJSON `json:"keys"`
}

func testMasterKey() []byte {
	return []byte("01234567890123456789012345678901")
}

// buildTestChain opens a fresh SQLite database, appends n real signed
// receipts, and writes a trust file covering every key used. It returns
// the database path, the trust file path, and the committed receipts.
func buildTestChain(t *testing.T, n int) (dbPath, trustPath string, receipts []receipt.Receipt) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "agentgate.db")

	database, err := agentgatedb.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("db.RunMigrations: %v", err)
	}

	store, err := signer.NewStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("signer.NewStore: %v", err)
	}
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}
	ledger := receipt.NewLedger(database, store)

	for i := 0; i < n; i++ {
		draft := receipt.Draft{
			HumanPrincipal: "user-1",
			AgentKeyID:     "agent-1",
			Service:        "github",
			Action:         "list_repos",
			PolicyDecision: "allow",
			StatusCode:     200,
			LatencyMS:      5,
		}
		r, err := ledger.Append(t.Context(), draft)
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		receipts = append(receipts, r)
	}

	keys, err := store.PublicKeys()
	if err != nil {
		t.Fatalf("PublicKeys: %v", err)
	}
	trusted := make([]trustedKeyJSON, 0, len(keys))
	for _, k := range keys {
		trusted = append(trusted, trustedKeyJSON{
			KID:           k.KID,
			PublicKeyHex:  hex.EncodeToString(k.PublicKey),
			ValidFromSeq:  k.ValidFromSeq,
			ValidUntilSeq: k.ValidUntilSeq,
		})
	}
	trustData, err := json.Marshal(trustFileJSON{Keys: trusted})
	if err != nil {
		t.Fatalf("marshal trust file: %v", err)
	}
	trustPath = filepath.Join(t.TempDir(), "trust.json")
	if err := os.WriteFile(trustPath, trustData, 0o600); err != nil {
		t.Fatalf("write trust file: %v", err)
	}
	return dbPath, trustPath, receipts
}

// dropAppendOnlyTriggers removes the receipts table's UPDATE/DELETE guard
// triggers so a test can simulate tampering that has defeated them.
func dropAppendOnlyTriggers(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TRIGGER IF EXISTS trg_receipts_no_update; DROP TRIGGER IF EXISTS trg_receipts_no_delete;`); err != nil {
		t.Fatal(err)
	}
}

func writeJSONLFile(t *testing.T, receipts []receipt.Receipt) string {
	t.Helper()
	var buf bytes.Buffer
	for _, r := range receipts {
		line, err := receipt.MarshalJSONLReceipt(r)
		if err != nil {
			t.Fatalf("MarshalJSONLReceipt: %v", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write jsonl file: %v", err)
	}
	return path
}

func TestRun_SQLite_ValidChainExitsZero(t *testing.T) {
	dbPath, trustPath, _ := buildTestChain(t, 5)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "sqlite", "--path", dbPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("PASS")) {
		t.Fatalf("stdout = %s, want a PASS line", stdout.String())
	}
}

// TestRun_SQLite_ModifiedRowExitsOne covers VER-05 end to end.
func TestRun_SQLite_ModifiedRowExitsOne(t *testing.T) {
	dbPath, trustPath, _ := buildTestChain(t, 5)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The append-only triggers block casual tampering; an attacker with
	// enough SQLite access to defeat them can still drop them first. The
	// point of this test is that cryptographic verification catches the
	// tamper even then.
	dropAppendOnlyTriggers(t, db)
	if _, err := db.Exec(`UPDATE receipts SET status_code = 404 WHERE seq = 3`); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "sqlite", "--path", dbPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// TestRun_SQLite_DeletedRowExitsOne covers VER-06 end to end.
func TestRun_SQLite_DeletedRowExitsOne(t *testing.T) {
	dbPath, trustPath, _ := buildTestChain(t, 5)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dropAppendOnlyTriggers(t, db)
	if _, err := db.Exec(`DELETE FROM receipts WHERE seq = 3`); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "sqlite", "--path", dbPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// TestRun_SQLite_InsertedRowExitsOne covers VER-07 end to end: a row
// spliced in from an entirely different, independently valid chain.
func TestRun_SQLite_InsertedRowExitsOne(t *testing.T) {
	dbPath, trustPath, _ := buildTestChain(t, 3)
	otherDBPath, _, otherReceipts := buildTestChain(t, 1)
	_ = otherDBPath

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	foreign := otherReceipts[0]
	if _, err := db.Exec(`
		INSERT INTO receipts (
			seq, format_version, timestamp_unix_ns, human_principal, agent_key_id,
			delegation_chain_json, service, action, params_sha256, policy_decision,
			status_code, latency_ms, error_code, prev_hash, entry_hash, signer_kid, signature
		) VALUES (4, 1, ?, ?, ?, '[]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		foreign.TimestampUnixNS, foreign.HumanPrincipal, foreign.AgentKeyID,
		foreign.Service, foreign.Action, foreign.ParamsSHA256[:], foreign.PolicyDecision,
		foreign.StatusCode, foreign.LatencyMS, foreign.Error, foreign.PrevHash[:],
		foreign.EntryHash[:], foreign.SignerKID, foreign.Signature[:],
	); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "sqlite", "--path", dbPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 1 && code != 2 {
		t.Fatalf("exit code = %d, want 1 or 2 (spliced foreign receipt); stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// TestRun_SQLite_ForgedSignatureExitsOne covers VER-08 end to end.
func TestRun_SQLite_ForgedSignatureExitsOne(t *testing.T) {
	dbPath, trustPath, receipts := buildTestChain(t, 3)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	dropAppendOnlyTriggers(t, db)
	forged := append([]byte{}, receipts[1].Signature[:]...)
	forged[0] ^= 0xFF
	if _, err := db.Exec(`UPDATE receipts SET signature = ? WHERE seq = 2`, forged); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "sqlite", "--path", dbPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_JSONL_ValidChainExitsZero(t *testing.T) {
	_, trustPath, receipts := buildTestChain(t, 4)
	jsonlPath := writeJSONLFile(t, receipts)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", jsonlPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
}

// TestRun_JSONL_MalformedLineExitsTwo covers VER-09.
func TestRun_JSONL_MalformedLineExitsTwo(t *testing.T) {
	_, trustPath, receipts := buildTestChain(t, 2)
	jsonlPath := writeJSONLFile(t, receipts)

	if err := os.WriteFile(jsonlPath, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", jsonlPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// TestRun_EmptySourceExitsTwo covers the other half of VER-09.
func TestRun_EmptySourceExitsTwo(t *testing.T) {
	_, trustPath, _ := buildTestChain(t, 1)
	emptyPath := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", emptyPath, "--trust-root", trustPath}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRun_MissingFlagsExitTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--source", "bogus", "--path", "x", "--trust-root", "y"}, &stdout, &stderr); code != 2 {
		t.Fatalf("bad --source: exit code = %d, want 2", code)
	}
	if code := run([]string{"--source", "jsonl", "--trust-root", "y"}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing --path: exit code = %d, want 2", code)
	}
	if code := run([]string{"--source", "jsonl", "--path", "x"}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing --trust-root: exit code = %d, want 2", code)
	}
}

// TestRun_ExpectedHead covers VER-11 end to end.
func TestRun_ExpectedHead(t *testing.T) {
	_, trustPath, receipts := buildTestChain(t, 4)
	jsonlPath := writeJSONLFile(t, receipts)
	last := receipts[len(receipts)-1]
	goodHead := hex.EncodeToString(last.EntryHash[:])

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--source", "jsonl", "--path", jsonlPath, "--trust-root", trustPath,
		"--expected-head", "4:" + goodHead,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("completeness: proven")) {
		t.Fatalf("stdout = %s, want a completeness-proven line", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"--source", "jsonl", "--path", jsonlPath, "--trust-root", trustPath,
		"--expected-head", "5:" + goodHead,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("mismatched expected head: exit code = %d, want 1", code)
	}
}

// buildTestExportJSONL builds a real chain via a Ledger, serves it through
// receipt.ExportHandler over httptest, and writes the resulting ndjson body
// (manifest + embedded keys + receipts) to a temp file. Returns the file
// path and the total number of receipts appended (the chain's true head).
func buildTestExportJSONL(t *testing.T, appendCount int, query string) (path string, headSeq int) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "agentgate.db")
	database, err := agentgatedb.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := agentgatedb.RunMigrations(database); err != nil {
		t.Fatalf("db.RunMigrations: %v", err)
	}
	store, err := signer.NewStore(database, testMasterKey())
	if err != nil {
		t.Fatalf("signer.NewStore: %v", err)
	}
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatalf("LoadOrCreateActive: %v", err)
	}
	ledger := receipt.NewLedger(database, store)
	for i := 0; i < appendCount; i++ {
		draft := receipt.Draft{
			HumanPrincipal: "user-1",
			AgentKeyID:     "agent-1",
			Service:        "github",
			Action:         "list_repos",
			PolicyDecision: "allow",
			StatusCode:     200,
			LatencyMS:      5,
		}
		if _, err := ledger.Append(t.Context(), draft); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	req := httptest.NewRequest("GET", "/v1/receipts/export?"+query, nil)
	w := httptest.NewRecorder()
	receipt.ExportHandler(database, store)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", w.Code, w.Body.String())
	}

	path = filepath.Join(t.TempDir(), "export.jsonl")
	if err := os.WriteFile(path, w.Body.Bytes(), 0o600); err != nil {
		t.Fatalf("write export file: %v", err)
	}
	return path, appendCount
}

// TestRun_JSONLExport_FullRangeSelfContained covers EXPT-04: a full-range
// export verifies with no --trust-root, using only its own embedded keys
// and manifest-derived anchor/expected-head, and reports "range: full".
func TestRun_JSONLExport_FullRangeSelfContained(t *testing.T) {
	jsonlPath, _ := buildTestExportJSONL(t, 5, "from=1")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", jsonlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("PASS")) {
		t.Fatalf("stdout = %s, want PASS", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("range: full")) {
		t.Fatalf("stdout = %s, want 'range: full'", stdout.String())
	}
}

// TestRun_JSONLExport_PartialRangeSelfContained covers EXPT-04 for a
// bounded, non-genesis-anchored export: it must still verify with no
// --trust-root and must report the range as partial.
func TestRun_JSONLExport_PartialRangeSelfContained(t *testing.T) {
	jsonlPath, _ := buildTestExportJSONL(t, 6, "from=3&to=5")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", jsonlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("PASS")) {
		t.Fatalf("stdout = %s, want PASS", stdout.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("range: partial")) {
		t.Fatalf("stdout = %s, want 'range: partial'", stdout.String())
	}
}

// TestRun_JSONLExport_TamperedManifestSignatureFailsClosed covers EXPT-03's
// binding guarantee: a manifest whose signature no longer matches its
// content must fail verification even though every individual receipt in
// the export is untouched and would verify on its own.
func TestRun_JSONLExport_TamperedManifestSignatureFailsClosed(t *testing.T) {
	jsonlPath, _ := buildTestExportJSONL(t, 4, "from=1")

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) == 0 || receipt.DetectJSONLLineType(lines[0]) != "manifest" {
		t.Fatal("expected the first line of the export to be the manifest")
	}

	manifest, err := receipt.ParseManifestLine(lines[0])
	if err != nil {
		t.Fatalf("ParseManifestLine: %v", err)
	}
	manifest.Signature[0] ^= 0xFF // flip a byte, preserving length and structure
	tampered, err := receipt.MarshalManifestLine(manifest)
	if err != nil {
		t.Fatalf("MarshalManifestLine: %v", err)
	}
	lines[0] = tampered

	tamperedPath := filepath.Join(t.TempDir(), "tampered.jsonl")
	if err := os.WriteFile(tamperedPath, bytes.Join(lines, []byte("\n")), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", tamperedPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
