package receipt

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

func TestAdvisorVerificationModes(t *testing.T) {
	l, store := newTestLedgerAndStore(t)
	d := testDraft("github", "write")
	d.AdvisorRecord = json.RawMessage(`{"outcome":"allow"}`)
	for i := 0; i < 2; i++ {
		if _, err := l.Append(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	ExportHandler(l.db, store)(w, httptest.NewRequest("GET", "/v1/receipts/export?from=1&records=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	bundle, err := receiptspec.ReadJSONL(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiptspec.VerifyBundle(bundle, nil, nil)
	if err != nil || !result.OK || result.UncheckedAdvisorRecords != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	delete(bundle.Records, 4)
	result, err = receiptspec.VerifyBundle(bundle, nil, nil)
	if err != nil || result.OK || result.FailedAtSeq != 4 {
		t.Fatalf("partial evidence accepted: %+v %v", result, err)
	}
	delete(bundle.Records, 2)
	result, err = receiptspec.VerifyBundle(bundle, nil, nil)
	if err != nil || !result.OK || result.UncheckedAdvisorRecords != 2 {
		t.Fatalf("chain-only count incorrect: %+v %v", result, err)
	}
	bundle.RequireAdvisorRecords = true
	result, err = receiptspec.VerifyBundle(bundle, nil, nil)
	if err != nil || result.OK {
		t.Fatal("strict mode accepted absent evidence")
	}
	bundle.RequireAdvisorRecords = false
	bundle.Receipts[1].Signature[0] ^= 1
	result, err = receiptspec.VerifyBundle(bundle, nil, nil)
	if err == nil && result.OK {
		t.Fatal("chain-only mode ignored an invalid signature")
	}
}
