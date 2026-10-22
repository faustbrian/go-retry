package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/go-resilience/v2"
	retry "github.com/faustbrian/go-retry/v3"
)

func currentScope(t *testing.T, clock *manualClock) (resilience.WorkBudgetScope, context.Context) {
	t.Helper()
	budget, err := resilience.NewBudget(resilience.BudgetConfig{MaxResources: 1, MaxScopes: 1,
		MaxAdditionalPerExecution: 1, MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1,
		AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
	if err != nil {
		t.Fatal(err)
	}
	scope, ctx, err := budget.Start(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !scope.Snapshot().Closed {
			if err := scope.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	return scope, ctx
}

func TestCanonicalRetryBudgetLineageAndRefusal(t *testing.T) {
	clock := newManualClock(time.Unix(700, 0))
	scope, ctx := currentScope(t, clock)
	policy := mustPolicy(t, retry.Config{Backoff: retry.Constant(0), MaxAttempts: 3, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true})
	var calls uint64
	result, err := retry.Do(ctx, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
		calls++
		attempt, ok := resilience.AttemptFromContext(ctx)
		if !ok || attempt.Ordinal != calls {
			t.Fatal("missing current lineage")
		}
		if calls == 1 && (attempt.Origin != resilience.OriginOriginal || attempt.ParentOrdinal != 0) {
			t.Fatal("original lineage")
		}
		if calls == 2 && (attempt.Origin != resilience.OriginRetry || attempt.ParentOrdinal != 1) {
			t.Fatal("retry lineage")
		}
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
	})
	var rejection *resilience.BudgetRejectionError
	if calls != 2 || result.Value != "" || result.Retry.Attempts != 2 || result.Retry.Reason != retry.ReasonWorkBudget ||
		!errors.Is(err, resilience.ErrBudgetRejected) || !errors.As(err, &rejection) || rejection.Reason != resilience.ReasonExecutionLimit {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
	if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 1 || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestCanonicalRetryBorrowsOuterPermit(t *testing.T) {
	clock := newManualClock(time.Unix(710, 0))
	scope, ctx := currentScope(t, clock)
	attached, original, permit, err := resilience.AdmitAttempt(ctx, resilience.OriginOriginal, 0, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := permit.Complete(); err != nil {
			t.Error(err)
		}
	}()
	policy := mustPolicy(t, retry.Config{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true})
	result, err := retry.Do(attached, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
		attempt, ok := resilience.AttemptFromContext(ctx)
		if !ok || attempt != original {
			t.Fatal("borrowed lineage changed")
		}
		return retry.AttemptResult[string]{Value: "accepted", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "accepted" || result.Retry.Attempts != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 0 || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestCanonicalRetryMissingOrClosedScopeDoesNotDispatch(t *testing.T) {
	for _, closed := range []bool{false, true} {
		clock := newManualClock(time.Unix(720, 0))
		ctx := context.Background()
		if closed {
			var scope resilience.WorkBudgetScope
			scope, ctx = currentScope(t, clock)
			if err := scope.Close(); err != nil {
				t.Fatal(err)
			}
		}
		policy := mustPolicy(t, retry.Config{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
			Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true})
		calls := 0
		result, err := retry.Do(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			return retry.AttemptResult[string]{Value: "wrong", Outcome: retry.OutcomeKnown}, nil
		})
		if err == nil || calls != 0 || result.Retry.Attempts != 0 || result.Retry.Reason != retry.ReasonWorkBudget || result.Outcome != retry.OutcomeNotDispatched {
			t.Fatalf("closed=%v calls=%d result=%+v err=%v", closed, calls, result, err)
		}
	}
}
