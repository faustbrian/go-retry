package retry_test

import (
	"testing"

	retry "github.com/faustbrian/go-retry/v3"
)

func TestOutcomeZeroValuesMeanNotDispatched(t *testing.T) {
	t.Parallel()

	var outcome retry.Outcome
	var attempt retry.AttemptResult[string]
	var result retry.ExecutionResult[string]
	if outcome != retry.OutcomeNotDispatched || attempt.Outcome != retry.OutcomeNotDispatched || attempt.Value != "" || result.Outcome != retry.OutcomeNotDispatched || result.Value != "" || result.Retry.Attempts != 0 || result.Retry.Reason != "" || len(result.Retry.History) != 0 {
		t.Fatalf("outcome=%d attempt=%+v result=%+v", outcome, attempt, result)
	}
}
