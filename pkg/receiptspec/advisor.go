package receiptspec

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
)

var ErrAdvisorBinding = errors.New("receipt: advisor companion binding invalid")

func IsAdvisorReceipt(r Receipt) bool {
	return r.Service == "advisor" && r.Action == "decision:advise"
}

// AdvisorPolicy maps detailed outcomes to the existing v1 policy enum.
func AdvisorPolicy(record []byte) (string, error) {
	var value struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(record, &value); err != nil {
		return "", err
	}
	switch value.Outcome {
	case "allow":
		return "allow", nil
	case "deny", "deny_by_advisor", "require_approval":
		return "deny", nil
	default:
		return "", ErrAdvisorBinding
	}
}

type actionBinding struct {
	Seq          string `json:"seq"`
	EntryHash    string `json:"entry_hash"`
	ParamsSHA256 string `json:"params_sha256"`
}

func bindingFor(r Receipt) actionBinding {
	return actionBinding{strconv.FormatUint(r.Seq, 10), hex.EncodeToString(r.EntryHash[:]), hex.EncodeToString(r.ParamsSHA256[:])}
}

// BindAdvisorRecord links the separate producer's record to an already sealed gateway receipt.
func BindAdvisorRecord(action Receipt, record []byte) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(record, &object); err != nil {
		return nil, err
	}
	if object == nil || IsAdvisorReceipt(action) {
		return nil, ErrAdvisorBinding
	}
	link, err := json.Marshal(bindingFor(action))
	if err != nil {
		return nil, err
	}
	object["action_receipt"] = link
	return CanonicalRecord(object)
}

func VerifyAdvisorRecord(action, companion Receipt, record []byte) error {
	if !IsAdvisorReceipt(companion) || IsAdvisorReceipt(action) || companion.Seq != action.Seq+1 ||
		companion.PrevHash != action.EntryHash {
		return ErrAdvisorBinding
	}
	if err := VerifyRecordBinding(companion, record); err != nil {
		return err
	}
	var object struct {
		ActionReceipt        actionBinding `json:"action_receipt"`
		OriginalParamsSHA256 string        `json:"original_params_sha256"`
	}
	if err := json.Unmarshal(record, &object); err != nil {
		return err
	}
	if object.ActionReceipt != bindingFor(action) {
		return ErrAdvisorBinding
	}
	if object.OriginalParamsSHA256 != "" && object.OriginalParamsSHA256 != bindingFor(action).ParamsSHA256 {
		return ErrAdvisorBinding
	}
	policy, err := AdvisorPolicy(record)
	if err != nil || policy != companion.PolicyDecision {
		return ErrAdvisorBinding
	}
	return nil
}

type recordLine struct {
	Type   string          `json:"type"`
	Seq    uint64          `json:"seq"`
	Record json.RawMessage `json:"record"`
}

// MarshalRecordLine adds a bundle line without changing any receipt's bytes.
func MarshalRecordLine(seq uint64, record []byte) ([]byte, error) {
	canonical, err := CanonicalRecord(json.RawMessage(record))
	if err != nil {
		return nil, err
	}
	return json.Marshal(recordLine{Type: "record", Seq: seq, Record: canonical})
}

func ParseRecordLine(line []byte) (uint64, json.RawMessage, error) {
	var value recordLine
	if err := json.Unmarshal(line, &value); err != nil {
		return 0, nil, err
	}
	if value.Type != "record" || value.Seq == 0 {
		return 0, nil, ErrAdvisorBinding
	}
	canonical, err := CanonicalRecord(value.Record)
	return value.Seq, json.RawMessage(canonical), err
}
