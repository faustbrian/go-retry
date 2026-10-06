package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	resiliencev1 "github.com/faustbrian/go-resilience"
	resiliencev2 "github.com/faustbrian/go-resilience/v2"
	"github.com/faustbrian/go-retry/v2"
)

func runVersionedRetry(t *testing.T, strict bool, ctx context.Context, clock *manualClock, operation func(context.Context) (string, error)) (string, retry.Result, error) {
	t.Helper()
	config := retry.Config{Backoff: retry.Constant(0), MaxAttempts: 3, Clock: clock,
		Sleeper: advancingSleeper{clock: clock}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true}
	if strict {
		policy, err := retry.NewPolicyStrict(config)
		if err != nil {
			t.Fatal(err)
		}
		result, err := retry.DoStrict(ctx, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
			value, err := operation(ctx)
			return retry.AttemptResult[string]{Value: value, Outcome: retry.OutcomeKnown}, err
		})
		return result.Value, result.Retry, err
	}
	policy, err := retry.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	return retry.Do(ctx, policy, operation)
}

func version2Scope(t *testing.T, ctx context.Context, clock *manualClock) (resiliencev2.WorkBudgetScope, context.Context) {
	t.Helper()
	budget, err := resiliencev2.NewBudget(resiliencev2.BudgetConfig{MaxResources: 1, MaxScopes: 1,
		MaxAdditionalPerExecution: 1, MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1,
		AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := resiliencev2.NewMetadata("logical", "lookup", "dependency")
	if err != nil {
		t.Fatal(err)
	}
	scope, attached, err := budget.Start(ctx, metadata)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := scope.Close(); err != nil {
			t.Errorf("close scope: %v", err)
		}
	})
	return scope, attached
}

func TestRetryVersion2BudgetComposition(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "legacy"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			clock := newManualClock(time.Unix(700, 0))
			scope, ctx := version2Scope(t, context.Background(), clock)
			calls := 0
			value, result, err := runVersionedRetry(t, strict, ctx, clock, func(ctx context.Context) (string, error) {
				calls++
				attempt, ok := resiliencev2.AttemptFromContext(ctx)
				if !ok || attempt.Ordinal != uint64(calls) {
					t.Fatal("missing version2 lineage")
				}
				if calls == 1 && (attempt.Origin != resiliencev2.OriginOriginal || attempt.ParentOrdinal != 0) {
					t.Fatal("wrong original lineage")
				}
				if calls == 2 && (attempt.Origin != resiliencev2.OriginRetry || attempt.ParentOrdinal != 1) {
					t.Fatal("wrong retry lineage")
				}
				return "", retry.Retryable(errors.New("temporary"))
			})
			var rejection *resiliencev2.BudgetRejectionError
			if value != "" || calls != 2 || result.Attempts != 2 || result.Reason != retry.ReasonWorkBudget ||
				!errors.Is(err, resiliencev2.ErrBudgetRejected) || !errors.As(err, &rejection) || rejection.Reason != resiliencev2.ReasonExecutionLimit {
				t.Fatalf("version2 refusal: calls=%d result=%+v error=%v", calls, result, err)
			}
			if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 1 || snapshot.AdditionalActive != 0 {
				t.Fatalf("snapshot=%+v", snapshot)
			}
		})
	}
}

func TestRetryVersion2BorrowedAttempt(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "legacy"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			clock := newManualClock(time.Unix(710, 0))
			scope, ctx := version2Scope(t, context.Background(), clock)
			attached, original, permit, err := resiliencev2.AdmitAttempt(ctx, resiliencev2.OriginOriginal, 0, clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := permit.Complete(); err != nil {
					t.Errorf("complete borrowed permit: %v", err)
				}
			}()
			value, result, err := runVersionedRetry(t, strict, attached, clock, func(ctx context.Context) (string, error) {
				current, ok := resiliencev2.AttemptFromContext(ctx)
				if !ok || current != original {
					t.Fatal("borrowed attempt changed")
				}
				return "accepted", nil
			})
			if err != nil || value != "accepted" || result.Attempts != 1 {
				t.Fatalf("borrowed result=%+v error=%v", result, err)
			}
			if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 0 || snapshot.AdditionalActive != 0 {
				t.Fatalf("snapshot=%+v", snapshot)
			}
		})
	}
}

func TestRetryRejectsDualBudgetVersions(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, v2First := range []bool{false, true} {
			name := "legacy"
			if strict {
				name = "strict"
			}
			if v2First {
				name += "/v2-first"
			} else {
				name += "/v1-first"
			}
			t.Run(name, func(t *testing.T) {
				clock := newManualClock(time.Unix(720, 0))
				budget, err := resiliencev1.NewBudget(resiliencev1.BudgetConfig{MaxResources: 1, MaxAdditionalPerExecution: 1,
					MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1, AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock})
				if err != nil {
					t.Fatal(err)
				}
				metadata, err := resiliencev1.NewMetadata("logical", "lookup", "dependency")
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				var scope2 resiliencev2.WorkBudgetScope
				if v2First {
					scope2, ctx = version2Scope(t, ctx, clock)
				}
				scope1, ctx, err := budget.Start(ctx, metadata)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := scope1.Close(); err != nil {
						t.Errorf("close v1 scope: %v", err)
					}
				}()
				if !v2First {
					scope2, ctx = version2Scope(t, ctx, clock)
				}
				before1, before2 := scope1.Snapshot(), scope2.Snapshot()
				calls := 0
				value, result, err := runVersionedRetry(t, strict, ctx, clock, func(context.Context) (string, error) { calls++; return "wrong", nil })
				if !errors.Is(err, retry.ErrInvalidPolicy) || value != "" || calls != 0 || result.Attempts != 0 || result.Reason != retry.ReasonWorkBudget {
					t.Fatalf("dual dispatch: calls=%d result=%+v error=%v", calls, result, err)
				}
				if scope1.Snapshot() != before1 || scope2.Snapshot() != before2 {
					t.Fatal("dual attachment charged budget")
				}
			})
		}
	}
}

func TestRetryVersion2CanceledBeforeDispatch(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "legacy"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			clock := newManualClock(time.Unix(730, 0))
			scope, attached := version2Scope(t, context.Background(), clock)
			ctx, cancel := context.WithCancel(attached)
			cancel()
			before := scope.Snapshot()
			calls := 0
			_, result, err := runVersionedRetry(t, strict, ctx, clock, func(context.Context) (string, error) { calls++; return "wrong", nil })
			if !errors.Is(err, context.Canceled) || calls != 0 || result.Attempts != 0 {
				t.Fatalf("canceled calls=%d result=%+v error=%v", calls, result, err)
			}
			if scope.Snapshot() != before {
				t.Fatal("cancellation charged budget")
			}
		})
	}
}

func TestRetryVersionedBudgetMissingScope(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "legacy"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			clock := newManualClock(time.Unix(740, 0))
			calls := 0
			value, result, err := runVersionedRetry(t, strict, context.Background(), clock, func(context.Context) (string, error) { calls++; return "wrong", nil })
			if !errors.Is(err, resiliencev1.ErrBudgetScopeRequired) || value != "" || calls != 0 || result.Attempts != 0 || result.Reason != retry.ReasonWorkBudget {
				t.Fatalf("missing scope: calls=%d result=%+v error=%v", calls, result, err)
			}
		})
	}
}
