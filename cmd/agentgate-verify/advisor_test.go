package main

import (
	"bytes"
	"encoding/json"
	"testing"

	agentgatedb "github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/signer"
)

func TestSQLiteAdvisorCompanionVerification(t *testing.T) {
	path, trust, _ := buildTestChain(t, 1)
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
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatal(err)
	}
	ledger := receipt.NewLedger(database, store)
	defer ledger.Close()
	_, err = ledger.Append(t.Context(), receipt.Draft{
		HumanPrincipal: "person", AgentKeyID: "agent", Service: "github", Action: "write",
		PolicyDecision: "deny", StatusCode: 409, AdvisorRecord: json.RawMessage(`{"outcome":"require_approval"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--source", "sqlite", "--path", path, "--trust-root", trust}
	var out, stderr bytes.Buffer
	if code := run(args, &out, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if _, err := database.Exec(`UPDATE receipt_records SET record_json='{"outcome":"allow"}' WHERE seq=3`); err != nil {
		t.Fatal(err)
	}
	if code := run(args, &out, &stderr); code != 1 {
		t.Fatalf("accepted tampered SQLite record: %d", code)
	}
}
