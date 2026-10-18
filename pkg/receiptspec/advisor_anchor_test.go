package receiptspec

import (
	"encoding/json"
	"testing"
)

func TestAdvisorIdentityRequiresReservedProducer(t *testing.T) {
	for _, tc := range []struct {
		service, action, key string
		want                 bool
	}{
		{"advisor", "decision:advise", "normal-agent", false},
		{"advisor", "decision:advise", "", false},
		{"advisor", "decision:advise", AdvisorAgentKeyID, true},
		{"github", "decision:advise", AdvisorAgentKeyID, false},
		{"advisor", "read", AdvisorAgentKeyID, false},
	} {
		if got := IsAdvisorReceipt(Receipt{Service: tc.service, Action: tc.action, AgentKeyID: tc.key}); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}

func TestAdvisorAnchorChecksBoundLink(t *testing.T) {
	action := Receipt{Seq: 7}
	action.EntryHash[0] = 1
	action.ParamsSHA256[0] = 2
	record, err := BindAdvisorRecord(action, []byte(`{"outcome":"allow"}`))
	if err != nil {
		t.Fatal(err)
	}
	companion := Receipt{Seq: 8, Service: "advisor", Action: "decision:advise", AgentKeyID: AdvisorAgentKeyID, PrevHash: action.EntryHash, PolicyDecision: "allow"}
	companion.ParamsSHA256, err = HashRecord(json.RawMessage(record))
	if err != nil {
		t.Fatal(err)
	}
	anchor := Anchor{Seq: 7, EntryHash: action.EntryHash}
	if err := VerifyAdvisorRecordAtAnchor(anchor, companion, record); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"seq", "entry_hash"} {
		var value map[string]any
		if err := json.Unmarshal(record, &value); err != nil {
			t.Fatal(err)
		}
		value["action_receipt"].(map[string]any)[field] = "wrong"
		tampered, err := CanonicalRecord(value)
		if err != nil {
			t.Fatal(err)
		}
		c := companion
		c.ParamsSHA256, err = HashRecord(json.RawMessage(tampered))
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyAdvisorRecordAtAnchor(anchor, c, tampered); err == nil {
			t.Fatalf("accepted changed %s with a matching record digest", field)
		}
	}
	wrongPrev := companion
	wrongPrev.PrevHash[0] ^= 1
	if err := VerifyAdvisorRecordAtAnchor(anchor, wrongPrev, record); err == nil {
		t.Fatal("accepted mismatched prev_hash")
	}
	if err := VerifyAdvisorRecordAtAnchor(Anchor{}, companion, record); err == nil {
		t.Fatal("accepted missing anchor")
	}
}
