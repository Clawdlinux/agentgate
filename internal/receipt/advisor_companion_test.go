package receipt

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestAdvisorCompanionPreservesActionAndExports(t *testing.T) {
	l, store := newTestLedgerAndStore(t)
	d := testDraft("github", "create_issue")
	d.AdvisorRecord = json.RawMessage(`{"outcome":"require_approval"}`)
	d.PolicyDecision = "deny"
	action, err := l.Append(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	if action.ParamsSHA256 != d.ParamsSHA256 {
		t.Fatal("changed action params digest")
	}
	for attempt := 0; attempt < 2; attempt++ {
		w := httptest.NewRecorder()
		ExportHandler(l.db, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?from=1&records=1", nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		bundle, err := receiptspec.ReadJSONL(strings.NewReader(w.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		if len(bundle.Receipts) != 2 || len(bundle.Records) != 1 {
			t.Fatalf("missing companion: %+v", bundle)
		}
		companion := bundle.Receipts[1]
		if companion.Service != "advisor" || companion.Action != "decision:advise" || companion.AgentKeyID != receiptspec.AdvisorAgentKeyID || companion.PolicyDecision != "deny" ||
			companion.PrevHash != action.EntryHash {
			t.Fatal("incorrect companion")
		}
		record := bundle.Records[companion.Seq]
		if err := receiptspec.VerifyRecordBinding(companion, record); err != nil {
			t.Fatal(err)
		}
		result, err := receiptspec.VerifyBundle(bundle, nil, nil)
		if err != nil || !result.OK {
			t.Fatalf("%+v %v", result, err)
		}
		bundle.Records[companion.Seq] = json.RawMessage(`{"outcome":"allow"}`)
		result, err = receiptspec.VerifyBundle(bundle, nil, nil)
		if err != nil || result.OK {
			t.Fatalf("accepted tampering: %+v %v", result, err)
		}
		delete(bundle.Records, companion.Seq)
		result, err = receiptspec.VerifyBundle(bundle, nil, nil)
		if err != nil || result.OK {
			t.Fatal("accepted missing companion record")
		}
		if attempt == 0 {
			if _, err := l.db.Exec(`CREATE TRIGGER reject_advisor BEFORE INSERT ON receipt_records BEGIN SELECT RAISE(ABORT, 'failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Append(t.Context(), d); err == nil {
				t.Fatal("ignored companion persistence failure")
			}
		}
	}
	head, _, _ := l.Head(t.Context())
	if head != 2 {
		t.Fatal("non-atomic companion commit")
	}
}
