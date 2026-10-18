package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/signer"
)

func advisorExports(t *testing.T) (string, string) {
	t.Helper()
	path, _, _ := buildTestChain(t, 0)
	database, err := agentgatedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := signer.NewStore(database, testMasterKey())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, private, err := store.LoadOrCreateActive(1)
	clear(private)
	if err != nil {
		t.Fatal(err)
	}
	ledger := receipt.NewLedger(database, store)
	defer ledger.Close()
	if _, err := ledger.Append(t.Context(), receipt.Draft{HumanPrincipal: "human", AgentKeyID: "agent", Service: "github", Action: "write",
		StatusCode: 200, PolicyDecision: "allow", AdvisorRecord: json.RawMessage(`{"outcome":"allow"}`)}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "default.jsonl"), filepath.Join(dir, "records.jsonl")}
	for i, query := range []string{"from=1", "from=1&records=1"} {
		w := httptest.NewRecorder()
		receipt.ExportHandler(database, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?"+query, nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if err := os.WriteFile(paths[i], w.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths[0], paths[1]
}

func TestDefaultAdvisorExportPassesWithUncheckedNote(t *testing.T) {
	plain, records := advisorExports(t)
	for _, tc := range []struct {
		name, path string
		flags      []string
		code       int
		note       bool
	}{
		{"default", plain, nil, 0, true},
		{"quiet", plain, []string{"--quiet"}, 0, true},
		{"strict_default", plain, []string{"--require-advisor-records"}, 1, false},
		{"strict_records", records, []string{"--require-advisor-records"}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			args := append([]string{"--source", "jsonl", "--path", tc.path}, tc.flags...)
			if code := run(args, &out, &stderr); code != tc.code {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
			if strings.Contains(out.String(), "unchecked") != tc.note {
				t.Fatalf("missing or unexpected unchecked note: %s", out.String())
			}
		})
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"--source", "jsonl", "--path", plain, "--format", "json"}, &out, &stderr); code != 0 {
		t.Fatalf("code=%d %s", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value["unchecked_advisor_records"] != float64(1) || value["ok"] != true {
		t.Fatalf("missing unchecked count: %s", out.String())
	}
}
