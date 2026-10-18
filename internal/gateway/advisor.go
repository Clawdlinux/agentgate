package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/Clawdlinux/agentgate/internal/advisor"
)

func (s *Server) checkAdvisor(ctx context.Context, att *attempt) *outcome {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, advisor.Timeout)
	defer cancel()
	// The task and payload are untrusted assertions, never delegated authority.
	state := advisor.State{"service": att.req.Service, "action": att.req.Action, "params": att.params}
	questions := advisor.FixedQuestions()
	answers, err := s.cfg.Advisor.Decide(ctx, state, questions)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = answers.Validate()
	}
	decision := s.cfg.AdvisorPolicy.Evaluate("allow", answers, err)
	evidence, bindErr := advisor.NewEvidence(state, questions, att.paramsHash, s.cfg.AdvisorPolicy, answers, err, time.Since(start).Microseconds(), decision)
	if bindErr != nil {
		return &outcome{status: http.StatusInternalServerError, body: ErrorResponse{Error: "advisor input cannot be recorded", Code: "advisor_unavailable"}, policyDecision: "deny", errorCode: "advisor_unavailable"}
	}
	att.advisorEvidence = evidence
	if decision == "allow" {
		return nil
	}
	code := "advisor_escalated"
	if err != nil {
		code = "advisor_unavailable"
	}
	status := http.StatusForbidden
	if decision == "require_approval" {
		status = http.StatusConflict
	}
	return &outcome{status: status, body: ErrorResponse{Error: decision, Code: code}, policyDecision: "deny", errorCode: code}
}
