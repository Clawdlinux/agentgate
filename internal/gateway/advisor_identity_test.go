package gateway

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/internal/db"
	"github.com/Clawdlinux/agentgate/internal/receipt"
	"github.com/Clawdlinux/agentgate/internal/signer"
	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestReservedAdvisorRequestsCannotPoisonHistory(t *testing.T) {
	s, _, _ := testSetup(t)
	database, err := db.Open(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	store, err := signer.NewStore(database, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := store.LoadOrCreateActive(1); err != nil {
		t.Fatal(err)
	}
	ledger := receipt.NewLedger(database, store)
	defer ledger.Close()
	s.cfg.Receipts = ledger
	for _, body := range []string{
		`{"service":"advisor","action":"decision:advise","on_behalf_of":"user-1"}`,
		`{"service":"advisor","action":"read","on_behalf_of":"user-1"}`,
		`{"service":"ADVISOR","action":"read","on_behalf_of":"user-1"}`,
		`{"service":"github","action":"decision:custom","on_behalf_of":"user-1"}`,
	} {
		req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("reserved request got %d: %s", w.Code, body)
		}
		head, _, err := ledger.Head(t.Context())
		if err != nil || head != 0 {
			t.Fatalf("reserved request committed a receipt: %d %v", head, err)
		}
	}
	req := httptest.NewRequest("POST", "/v1/act", strings.NewReader(`{"service":"github","action":"unknown","on_behalf_of":"user-1"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	s.ServeHTTP(httptest.NewRecorder(), req)
	w := httptest.NewRecorder()
	receipt.ExportHandler(database, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?from=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	bundle, err := receiptspec.ReadJSONL(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiptspec.VerifyBundle(bundle, nil, nil)
	if err != nil || !result.OK || result.VerifiedCount != 1 {
		t.Fatalf("%+v %v", result, err)
	}
}
