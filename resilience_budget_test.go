package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/go-resilience/v2"
	"github.com/faustbrian/go-retry/v3"
)

func TestRetryConsumesAttachedResilienceBudget(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(500, 0))
	budget, err := resilience.NewBudget(resilience.BudgetConfig{
		MaxResources:              1,
		MaxAdditionalPerExecution: 1,
		MaxConcurrentAdditional:   1,
		MaxAdditionalPerWindow:    1,
		AdditionalWindow:          time.Minute,
		PermitTTL:                 time.Minute,
		Clock:                     clock, MaxScopes: 1,
	})
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
	if err != nil {
		t.Fatalf("new metadata: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start budget: %v", err)
	}
	policy := mustPolicy(t, retry.Config{
		Backoff:             retry.Constant(0),
		MaxAttempts:         3,
		Clock:               clock,
		Sleeper:             advancingSleeper{clock: clock},
		Classifier:          retry.RetryableClassifier(),
		UseResilienceBudget: true,
	})

	calls := 0
	execution36, executionError36 := retry.Do(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		calls++
		return retry.AttemptResult[string]{Value: "", Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("downstream"))
	})
	_, result, err := execution36.Value, execution36.Retry, executionError36

	var budgetErr *retry.BudgetError
	if !errors.As(err, &budgetErr) || !errors.Is(err, resilience.ErrBudgetRejected) || budgetErr.Kind != retry.BudgetWork {
		t.Fatalf("error = %v", err)
	}
	if calls != 2 || result.Attempts != 2 || result.Reason != retry.ReasonWorkBudget {
		t.Fatalf("calls = %d, result = %+v", calls, result)
	}
	if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 1 || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestRetryResilienceBudgetRequiresAttachedScope(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(600, 0))
	policy := mustPolicy(t, retry.Config{
		Backoff:             retry.Constant(0),
		MaxAttempts:         2,
		Clock:               clock,
		Sleeper:             advancingSleeper{clock: clock},
		Classifier:          retry.RetryableClassifier(),
		UseResilienceBudget: true,
	})
	called := false
	execution37, executionError37 := retry.Do(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		called = true
		return retry.AttemptResult[string]{Value: "", Outcome: retry.OutcomeKnown}, nil
	})
	_, result, err := execution37.Value, execution37.Retry, executionError37

	if !errors.Is(err, resilience.ErrBudgetScopeRequired) || called || result.Reason != retry.ReasonWorkBudget {
		t.Fatalf("called = %v, result = %+v, error = %v", called, result, err)
	}
}

func TestRetryReusesThePhysicalAttemptAlreadyInContext(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(700, 0))
	budget, err := resilience.NewBudget(resilience.BudgetConfig{
		MaxResources: 1, MaxAdditionalPerExecution: 1,
		MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1,
		AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock, MaxScopes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
	if err != nil {
		t.Fatal(err)
	}
	_, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	attemptCtx, original, permit, err := resilience.AdmitAttempt(ctx, resilience.OriginOriginal, 0, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	policy := mustPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: advancingSleeper{clock: clock}, Classifier: retry.RetryableClassifier(),
		UseResilienceBudget: true,
	})
	execution38, executionError38 := retry.Do(attemptCtx, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
		current, ok := resilience.AttemptFromContext(ctx)
		if !ok || current != original {
			t.Fatalf("current attempt = (%+v, %v), want %+v", current, ok, original)
		}
		return retry.AttemptResult[string]{Value: "ok", Outcome: retry.OutcomeKnown}, nil
	})
	_, result, err := execution38.Value, execution38.Retry, executionError38

	if err != nil || result.Attempts != 1 || result.Reason != retry.ReasonSucceeded {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatalf("complete original: %v", err)
	}
}
