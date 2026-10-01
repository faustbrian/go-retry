package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/go-resilience"
	"github.com/faustbrian/go-retry/v2"
)

func TestDoCompletesPermitWhenCancellationLandsAfterAdmission(t *testing.T) {
	for _, acquire := range []int{1, 2} {
		t.Run(map[int]string{1: "original", 2: "retry"}[acquire], func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			scope := &postAdmissionCancelScope{cancelOnAcquire: acquire, cancel: cancel}
			ctx, err := resilience.WithBudgetScope(base, scope)
			if err != nil {
				t.Fatal(err)
			}
			policy := mustPolicy(t, retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 2, Clock: newManualClock(time.Unix(650, 0)),
				Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true,
			})
			calls := 0
			value, result, executionErr := retry.Do(ctx, policy, func(context.Context) (string, error) {
				calls++
				return "", retry.Retryable(errors.New("temporary"))
			})
			var canceled *retry.CanceledError
			if value != "" || calls != acquire-1 || result.Attempts != uint(acquire-1) || result.Reason != retry.ReasonCanceled || !errors.As(executionErr, &canceled) || !errors.Is(executionErr, context.Canceled) {
				t.Fatalf("calls=%d value=%q result=%+v error=%v", calls, value, result, executionErr)
			}
			if len(scope.permits) != acquire {
				t.Fatalf("admitted permits=%d, want %d", len(scope.permits), acquire)
			}
			for index, permit := range scope.permits {
				if permit.completions != 1 {
					t.Fatalf("permit %d completions=%d, want 1", index, permit.completions)
				}
			}
		})
	}
}

func TestDoCompletesUndispatchedPermitOnElapsedExitAndClockPanic(t *testing.T) {
	t.Run("elapsed exit", func(t *testing.T) {
		start := time.Unix(660, 0)
		clock := &sequenceClock{times: []time.Time{start, start, start.Add(2 * time.Second), start.Add(2 * time.Second)}}
		scope := &postAdmissionCancelScope{cancelOnAcquire: 99}
		ctx, err := resilience.WithBudgetScope(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		policy := mustPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 1, MaxElapsed: time.Second,
			Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true,
		})
		calls := 0
		value, result, executionErr := retry.Do(ctx, policy, func(context.Context) (string, error) {
			calls++
			return "unexpected", nil
		})
		var budget *retry.BudgetError
		if value != "" || calls != 0 || result.Attempts != 0 || result.Reason != retry.ReasonElapsedBudget || !errors.As(executionErr, &budget) || budget.Kind != retry.BudgetElapsed || !errors.Is(executionErr, context.DeadlineExceeded) {
			t.Fatalf("calls=%d value=%q result=%+v error=%v", calls, value, result, executionErr)
		}
		if len(scope.permits) != 1 || scope.permits[0].completions != 1 {
			t.Fatalf("admitted=%d, permit completions must be 1", len(scope.permits))
		}
	})
	t.Run("clock panic", func(t *testing.T) {
		want := errors.New("owned clock panic")
		clock := &panicAfterAdmissionClock{now: time.Unix(670, 0), panicValue: want}
		scope := &postAdmissionCancelScope{cancelOnAcquire: 99}
		ctx, err := resilience.WithBudgetScope(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		policy := mustPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 1, MaxElapsed: time.Second,
			Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true,
		})
		calls := 0
		defer func() {
			//nolint:errorlint // Caller panic identity remains part of the legacy contract.
			if recovered := recover(); recovered != want || calls != 0 || len(scope.permits) != 1 || scope.permits[0].completions != 1 {
				t.Fatalf("recovered=%v calls=%d admitted=%d", recovered, calls, len(scope.permits))
			}
		}()
		_, _, _ = retry.Do(ctx, policy, func(context.Context) (string, error) {
			calls++
			return "unexpected", nil
		})
	})
}

func TestDoCancelsAttemptContextWhenOperationOrPermitPanics(t *testing.T) {
	want := errors.New("owned application panic")
	for _, role := range []string{"operation", "permit"} {
		t.Run(role, func(t *testing.T) {
			clock := &cancelTrackingTimeoutClock{now: time.Unix(680, 0)}
			ctx := context.Background()
			var permit *panickingCompletionPermit
			if role == "permit" {
				permit = &panickingCompletionPermit{panicValue: want}
				var err error
				ctx, err = resilience.WithBudgetScope(ctx, fixedPermitScope{permit: permit})
				if err != nil {
					t.Fatal(err)
				}
			}
			policy := mustPolicy(t, retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 1, AttemptTimeout: time.Second,
				Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: role == "permit",
			})
			defer func() {
				permitCalls := 0
				if permit != nil {
					permitCalls = permit.calls
				}
				//nolint:errorlint // Caller panic identity remains part of the legacy contract.
				if recovered := recover(); recovered != want || clock.timeouts != 1 || clock.cancels != 1 || permitCalls != map[string]int{"operation": 0, "permit": 1}[role] {
					t.Fatalf("recovered=%v timeouts=%d cancels=%d permit=%d", recovered, clock.timeouts, clock.cancels, permitCalls)
				}
			}()
			_, _, _ = retry.Do(ctx, policy, func(context.Context) (string, error) {
				if role == "operation" {
					panic(want)
				}
				return "done", nil
			})
		})
	}
}
