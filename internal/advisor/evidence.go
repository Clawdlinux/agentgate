package advisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/Clawdlinux/agentgate/pkg/receiptspec"
)

// Evidence uses decimal strings because decision records reject fractional JSON numbers.
type Evidence struct {
	RecordVersion            int               `json:"record_version"`
	RecordType               string            `json:"record_type"`
	ParamsSHA256             string            `json:"original_params_sha256"`
	InputStateSHA256         string            `json:"input_state_sha256"`
	OriginalInputStateSHA256 string            `json:"original_input_state_sha256,omitempty"`
	Truncated                bool              `json:"truncated"`
	InputRejected            bool              `json:"input_rejected"`
	Questions                Questions         `json:"questions"`
	Thresholds               map[string]string `json:"thresholds"`
	UnavailablePolicy        string            `json:"advisor_unavailable"`
	Unavailable              bool              `json:"unavailable"`
	Failure                  string            `json:"failure,omitempty"`
	LatencyUS                int64             `json:"latency_us"`
	Outcome                  string            `json:"outcome"`
	PolicyDecision           string            `json:"policy_decision"`
	ErrorCode                string            `json:"error_code"`
	Attempts                 []EvidenceTier    `json:"attempts"`
}

type EvidenceTier struct {
	Tier          string            `json:"tier"`
	ModelID       string            `json:"model_id"`
	Revision      string            `json:"revision"`
	WeightsSHA256 string            `json:"weights_sha256"`
	Probabilities map[string]string `json:"probabilities"`
	Confidence    string            `json:"confidence"`
	LatencyUS     int64             `json:"latency_us"`
}

func decimal(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func NewEvidence(state State, questions Questions, params [32]byte, p Policy, answers Answers, callErr error, latencyUS int64, outcome string) (*Evidence, error) {
	stateJSON, err := MarshalJSON(state)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(stateJSON)
	e := &Evidence{
		RecordVersion: 1, RecordType: "agentgate_advisor", ParamsSHA256: hex.EncodeToString(params[:]),
		InputStateSHA256: hex.EncodeToString(digest[:]), Questions: questions,
		Thresholds:        map[string]string{"approval": decimal(p.ApprovalThreshold), "deny": decimal(p.DenyThreshold), "confidence": decimal(p.ConfidenceThreshold)},
		UnavailablePolicy: p.Unavailable, Unavailable: IsUnavailable(callErr), LatencyUS: latencyUS, Outcome: outcome, Attempts: []EvidenceTier{},
		Truncated: answers.Truncated, InputRejected: answers.Truncated || (callErr != nil && !IsUnavailable(callErr)),
	}
	if callErr != nil {
		e.Failure = "advisor_unavailable"
		if !IsUnavailable(callErr) {
			e.Failure = "advisor_input_rejected"
		}
	}
	if len(answers.Attempts) > 0 {
		e.Thresholds["cascade"] = decimal(answers.CascadeThreshold)
	}
	for _, a := range answers.Attempts {
		t := EvidenceTier{Tier: a.Tier, ModelID: a.ModelID, Revision: a.Revision, WeightsSHA256: a.WeightsSHA256,
			Confidence: decimal(a.Confidence), LatencyUS: a.LatencyUS, Probabilities: map[string]string{}}
		for k, v := range a.Probabilities {
			t.Probabilities[k] = decimal(v)
		}
		e.Attempts = append(e.Attempts, t)
	}
	return e, nil
}

func (e *Evidence) Record(policyDecision, errorCode string) (json.RawMessage, error) {
	e.PolicyDecision = policyDecision
	e.ErrorCode = errorCode
	canonical, err := receiptspec.CanonicalRecord(e)
	return json.RawMessage(canonical), err
}
