package receipt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestEvidenceBindingAndAtomicCommit(t *testing.T) {
	l := newTestLedger(t)
	d := testDraft("github", "create_issue")
	d.Record = json.RawMessage(`{"outcome":"require_approval","probability":"0.875"}`)
	var err error
	d.ParamsSHA256, err = receiptspec.HashRecord(d.Record)
	if err != nil {
		t.Fatal(err)
	}

	d.PolicyDecision = "deny"
	d.Error = "advisor_escalated"
	r, err := l.Append(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	record, err := l.Record(t.Context(), r.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if err := receiptspec.VerifyRecordBinding(r, record); err != nil {
		t.Fatal(err)
	}
	bad := d
	bad.Record = json.RawMessage(`{"outcome":"allow"}`)
	if _, err := l.Append(t.Context(), bad); err == nil {
		t.Fatal("accepted unbound record")
	}
	if _, err := l.db.Exec(`CREATE TRIGGER reject_evidence BEFORE INSERT ON receipt_records BEGIN SELECT RAISE(ABORT, 'evidence failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(t.Context(), d); err == nil {
		t.Fatal("ignored evidence failure")
	}
	head, _, err := l.Head(t.Context())
	if err != nil || head != 1 {
		t.Fatalf("non-atomic append: head=%d error=%v", head, err)
	}
}

func TestEvidenceEndpoint(t *testing.T) {
	l := newTestLedger(t)
	d := testDraft("github", "create_issue")
	d.Record = json.RawMessage(`{"outcome":"allow"}`)
	d.ParamsSHA256, _ = receiptspec.HashRecord(d.Record)
	if _, err := l.Append(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/receipts/{seq}/evidence", EvidenceHandler(l))
	for _, tc := range []struct {
		seq    string
		status int
	}{{"1", 200}, {"2", 404}, {"0", 400}, {"not-a-number", 400}} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/receipts/"+tc.seq+"/evidence", nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.seq, w.Code)
		}
	}
	if _, err := l.db.Exec(`UPDATE receipt_records SET record_json='{"outcome":"deny"}' WHERE seq=1`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/receipts/1/evidence", nil))
	if w.Code != 500 {
		t.Fatal("returned tampered evidence")
	}
}
