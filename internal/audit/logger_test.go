package audit

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			agent_key_id TEXT NOT NULL,
			service TEXT NOT NULL,
			action TEXT NOT NULL,
			user_id TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			error TEXT
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLogger_WriteAndQuery(t *testing.T) {
	db := setupAuditDB(t)
	logger := NewLogger(db, nil)

	logger.Log(Entry{
		AgentKeyID: "key-1",
		Service:    "stripe",
		Action:     "list_invoices",
		UserID:     "user-42",
		StatusCode: 200,
		LatencyMs:  150,
	})

	logger.Log(Entry{
		AgentKeyID: "key-1",
		Service:    "github",
		Action:     "list_repos",
		UserID:     "user-42",
		StatusCode: 401,
		LatencyMs:  50,
		Error:      "token expired",
	})

	// Wait for async drain.
	time.Sleep(100 * time.Millisecond)
	logger.Close()

	entries, err := Query(db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
}
