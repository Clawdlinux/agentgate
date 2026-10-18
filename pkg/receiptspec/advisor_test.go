package receiptspec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdvisorRecordChecksActionLink(t *testing.T) {
	action := Receipt{Seq: 1}
	action.EntryHash[0] = 1
	action.ParamsSHA256[0] = 2
	record, err := BindAdvisorRecord(action, []byte(`{"outcome":"require_approval"}`))
	if err != nil {
		t.Fatal(err)
	}
	companion := Receipt{Seq: 2, Service: "advisor", Action: "decision:advise", PrevHash: action.EntryHash, PolicyDecision: "deny"}
	companion.ParamsSHA256, err = HashRecord(json.RawMessage(record))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAdvisorRecord(action, companion, record); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"seq", "entry_hash", "params_sha256"} {
		var value map[string]any
		if err := json.Unmarshal(record, &value); err != nil {
			t.Fatal(err)
		}
		value["action_receipt"].(map[string]any)[field] = "wrong"
		tampered, err := CanonicalRecord(value)
		if err != nil {
			t.Fatal(err)
		}
		companion.ParamsSHA256, err = HashRecord(json.RawMessage(tampered))
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAdvisorRecord(action, companion, tampered); err == nil {
			t.Fatalf("accepted changed %s with valid record hash", field)
		}
	}
}

func TestRecordJSONLRejectsDuplicates(t *testing.T) {
	line, err := MarshalRecordLine(2, []byte(`{"outcome":"allow"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSONL(strings.NewReader(string(line) + "\n" + string(line))); err == nil {
		t.Fatal("accepted duplicate records")
	}
}
