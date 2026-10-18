// Package advisor provides an isolated, escalate-only second opinion.
package advisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

const Timeout = 150 * time.Millisecond

type State map[string]any

type Question struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type Questions map[string]Question

func FixedQuestions() Questions {
	return Questions{
		"task_consistent": {Text: "Is this action consistent with the stated task?", Kind: "yes_no"},
		"personal_data":   {Text: "Does the payload contain personal data?", Kind: "yes_no"},
		"risk":            {Text: "Risk score", Kind: "score"},
	}
}

type TierAnswer struct {
	Tier          string             `json:"tier"`
	ModelID       string             `json:"model_id"`
	Revision      string             `json:"revision"`
	WeightsSHA256 string             `json:"weights_sha256"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	LatencyUS     int64              `json:"latency_us"`
}

type Answers struct {
	Attempts         []TierAnswer `json:"attempts"`
	CascadeThreshold float64      `json:"cascade_threshold"`
	Truncated        bool         `json:"-"`
}

type Advisor interface {
	Decide(context.Context, State, Questions) (Answers, error)
}

type NoopAdvisor struct{}

func (NoopAdvisor) Decide(context.Context, State, Questions) (Answers, error) {
	digest := sha256.Sum256([]byte("agentgate/noop-v1:no-weights"))
	identity := hex.EncodeToString(digest[:])
	return Answers{CascadeThreshold: .8, Attempts: []TierAnswer{{
		Tier: "A", ModelID: "agentgate/noop-v1", Revision: identity, WeightsSHA256: identity,
		Confidence: 1, Probabilities: map[string]float64{"task_consistent": 1, "personal_data": 0, "risk": 0},
	}}}, nil
}

type Policy struct {
	ApprovalThreshold   float64
	DenyThreshold       float64
	ConfidenceThreshold float64
	Unavailable         string
}

func DefaultPolicy() Policy {
	return Policy{ApprovalThreshold: .6, DenyThreshold: .9, ConfidenceThreshold: .8, Unavailable: "allow_with_flag"}
}

func probability(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

func (p Policy) Validate() error {
	if !probability(p.ApprovalThreshold) || !probability(p.DenyThreshold) || !probability(p.ConfidenceThreshold) ||
		p.ApprovalThreshold >= p.DenyThreshold {
		return errors.New("advisor: invalid thresholds")
	}
	switch p.Unavailable {
	case "allow_with_flag", "require_approval", "deny":
		return nil
	default:
		return errors.New("advisor: invalid advisor_unavailable")
	}
}

var revisionPattern = regexp.MustCompile(`^([a-f0-9]{40}|[a-f0-9]{64})$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (a Answers) Validate() error {
	if !probability(a.CascadeThreshold) || a.CascadeThreshold == 0 {
		return errors.New("advisor: invalid cascade threshold")
	}
	if len(a.Attempts) < 1 || len(a.Attempts) > 3 {
		return errors.New("advisor: invalid tier count")
	}
	for i, t := range a.Attempts {
		if t.Tier != string(rune('A'+i)) || t.ModelID == "" || len(t.ModelID) > 256 ||
			!revisionPattern.MatchString(t.Revision) || !digestPattern.MatchString(t.WeightsSHA256) ||
			!probability(t.Confidence) || t.LatencyUS < 0 || t.LatencyUS > Timeout.Microseconds() {
			return fmt.Errorf("advisor: invalid tier %d metadata", i)
		}
		if len(t.Probabilities) != 3 {
			return errors.New("advisor: invalid probability set")
		}
		for k := range FixedQuestions() {
			v, ok := t.Probabilities[k]
			if !ok || !probability(v) {
				return errors.New("advisor: invalid probability")
			}
		}
	}
	return nil
}

// Evaluate never grants authority. Deterministic denials remain unchanged.
func (p Policy) Evaluate(base string, a Answers, err error) string {
	if base != "allow" {
		return base
	}
	if err == nil {
		err = a.Validate()
	}
	if err != nil {
		if p.Unavailable == "deny" {
			return "deny_by_advisor"
		}
		if a.Validate() == nil {
			if a.risk() >= p.DenyThreshold {
				return "deny_by_advisor"
			}
			if a.risk() >= p.ApprovalThreshold {
				return "require_approval"
			}
		}
		if !IsUnavailable(err) || a.Truncated {
			return "require_approval"
		}
		switch p.Unavailable {
		case "require_approval":
			return "require_approval"
		default:
			return "allow"
		}
	}
	// Keep a high-risk observation from an earlier tier. Later tiers cannot erase it.
	score := a.risk()
	if score >= p.DenyThreshold {
		return "deny_by_advisor"
	}
	if a.Truncated && p.Unavailable == "deny" {
		return "deny_by_advisor"
	}
	if a.Truncated || score >= p.ApprovalThreshold || a.Attempts[len(a.Attempts)-1].Confidence < p.ConfidenceThreshold {
		return "require_approval"
	}
	return "allow"
}

func (a Answers) risk() float64 {
	score := 0.0
	for _, t := range a.Attempts {
		score = math.Max(score, math.Max(t.Probabilities["risk"], math.Max(1-t.Probabilities["task_consistent"], t.Probabilities["personal_data"])))
	}
	return score
}
