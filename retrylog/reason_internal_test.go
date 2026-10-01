package retrylog

import (
	"testing"

	"github.com/faustbrian/go-retry/v2"
)

func TestSharedWorkBudgetReasonRemainsObservable(t *testing.T) {
	t.Parallel()

	if got := reason(retry.ReasonWorkBudget); got != string(retry.ReasonWorkBudget) {
		t.Fatalf("work budget reason = %q", got)
	}
}
