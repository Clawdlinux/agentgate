package advisor

import (
	"context"
	"testing"
)

func TestUnavailableTierCannotEraseCompletedRisk(t *testing.T) {
	answer := testAnswers()
	answer.Attempts[0].Probabilities["risk"] = .95
	if got := DefaultPolicy().Evaluate("allow", answer, context.DeadlineExceeded); got != "deny_by_advisor" {
		t.Fatalf("lost completed risk: %s", got)
	}
}
