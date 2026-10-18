/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestRunManifestGoldenVectors(t *testing.T) {
	dir := filepath.Join("..", "..", "pkg", "receiptspec", "testdata", "v1")
	data, err := os.ReadFile(filepath.Join(dir, "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Vectors []struct {
			Name     string `json:"name"`
			JSONL    string `json:"jsonl"`
			Keys     string `json:"keys"`
			Expected struct {
				Class    string `json:"class"`
				Complete bool   `json:"complete"`
			} `json:"expected"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, v := range catalog.Vectors {
		if v.Expected.Class != "ok" && !strings.HasPrefix(v.Expected.Class, "manifest_") {
			continue
		}
		t.Run(v.Name, func(t *testing.T) {
			for _, explicitTrust := range []bool{false, true} {
				for _, format := range []string{"text", "json"} {
					args := []string{"--source", "jsonl", "--path", filepath.Join(dir, v.JSONL), "--format", format}
					if explicitTrust {
						args = append(args, "--trust-root", filepath.Join(dir, v.Keys))
					}
					var stdout, stderr bytes.Buffer
					code := run(args, &stdout, &stderr)
					wantCode := 1
					if v.Expected.Class == "ok" {
						wantCode = 0
					}
					if code != wantCode {
						t.Fatalf("trust=%v format=%s: exit %d, want %d; stdout=%s stderr=%s",
							explicitTrust, format, code, wantCode, stdout.String(), stderr.String())
					}
					if format == "json" {
						var got jsonResult
						if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
							t.Fatal(err)
						}
						if got.OK != (wantCode == 0) || got.Complete != v.Expected.Complete ||
							(wantCode == 1 && got.Reason != v.Expected.Class) {
							t.Fatalf("trust=%v: result %+v, want %+v", explicitTrust, got, v.Expected)
						}
					} else if wantCode == 1 && !strings.Contains(stderr.String(), "reason="+v.Expected.Class) {
						t.Fatalf("missing reason in stderr: %s", stderr.String())
					}
				}
			}
		})
	}
}

// A second producer that only uses pkg/receiptspec must verify with this command.
func TestRunVerifiesReceiptspecChain(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))
	s := receiptspec.NewEd25519Signer(priv)
	c := receiptspec.NewChain(s, 0, [32]byte{})
	record, err := receiptspec.HashRecord(map[string]any{"outcome": "require_approval", "record_version": 1})
	if err != nil {
		t.Fatal(err)
	}
	var receipts []receiptspec.Receipt
	for i := 0; i < 5; i++ {
		r, err := c.Next(receiptspec.Fields{
			TimestampUnixNS: uint64(1_700_000_000_000_000_000 + i),
			HumanPrincipal:  "operator@cluster",
			AgentKeyID:      "operator",
			Service:         "operator",
			Action:          "decision:scale",
			ParamsSHA256:    record,
			PolicyDecision:  receiptspec.DecisionDeny,
			StatusCode:      202,
		})
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, r)
	}

	dir := t.TempDir()
	var buf bytes.Buffer
	if err := receiptspec.WriteJSONL(&buf, receipts); err != nil {
		t.Fatal(err)
	}
	chainPath := filepath.Join(dir, "chain.jsonl")
	if err := os.WriteFile(chainPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	trust, _ := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kid": s.KID(), "public_key_hex": hex.EncodeToString(priv.Public().(ed25519.PublicKey)), "valid_from_seq": 1,
	}}})
	trustPath := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(trustPath, trust, 0o600); err != nil {
		t.Fatal(err)
	}

	head := receipts[4]
	var stdout, stderr bytes.Buffer
	code := run([]string{"--source", "jsonl", "--path", chainPath, "--trust-root", trustPath,
		"--expected-head", "5:" + hex.EncodeToString(head.EntryHash[:])}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr.String())
	}

	tampered := bytes.Replace(buf.Bytes(), []byte(`"latency_ms":0`), []byte(`"latency_ms":1`), 1)
	if err := os.WriteFile(chainPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--source", "jsonl", "--path", chainPath, "--trust-root", trustPath}, &stdout, &stderr); code != 1 {
		t.Fatalf("tampered exit %d, want 1", code)
	}
}
