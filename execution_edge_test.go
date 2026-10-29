package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v3"
)

func TestExecutionBudgetsStopBeforeSleeping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config func(*manualClock) retry.Config
		kind   retry.BudgetKind
		reason retry.Reason
	}{
		{"elapsed", func(clock *manualClock) retry.Config { return baseConfig(clock, retry.Config{MaxElapsed: time.Second}) }, retry.BudgetElapsed, retry.ReasonElapsedBudget},
		{"sleep", func(clock *manualClock) retry.Config { return baseConfig(clock, retry.Config{MaxSleep: time.Second}) }, retry.BudgetSleep, retry.ReasonSleepBudget},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newManualClock(time.Unix(100, 0))
			policy := mustPolicy(t, test.config(clock))
			execution10, executionError10 := retry.Do(context.Background(), policy, alwaysRetryable)
			_, result, err := execution10.Value, execution10.Retry, executionError10
			var budget *retry.BudgetError
			if !errors.As(err, &budget) || budget.Kind != test.kind || result.Reason != test.reason {
				t.Fatalf("error=%v result=%+v", err, result)
			}
			if result.Attempts != 1 || result.Elapsed != 0 {
				t.Fatalf("unexpected execution before budget: %+v", result)
			}
		})
	}
}

func TestDelayBoundsAreAppliedBeforeSleep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backoff  time.Duration
		minimum  time.Duration
		maximum  time.Duration
		expected time.Duration
	}{
		{"minimum", 0, time.Second, 0, time.Second},
		{"maximum", 10 * time.Second, 0, 3 * time.Second, 3 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newManualClock(time.Unix(100, 0))
			policy := mustPolicy(t, retry.Config{
				Backoff: retry.Constant(test.backoff), MaxAttempts: 2,
				MinDelay: test.minimum, MaxDelay: test.maximum,
				Clock: clock, Sleeper: advancingSleeper{clock},
				Classifier: retry.RetryableClassifier(),
			})
			execution11, executionError11 := retry.Do(context.Background(), policy, alwaysRetryable)
			_, result, _ := execution11.Value, execution11.Retry, executionError11
			if result.FinalDelay != test.expected || result.Elapsed != test.expected {
				t.Fatalf("got %+v, want delay %s", result, test.expected)
			}
		})
	}
}

func TestCancellationPrecedence(t *testing.T) {
	t.Parallel()

	cause := errors.New("temporary")
	t.Run("before attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		clock := newManualClock(time.Unix(100, 0))
		policy := mustPolicy(t, baseConfig(clock, retry.Config{}))
		calls := 0
		execution12, executionError12 := retry.Do(ctx, policy, func(context.Context) (retry.AttemptResult[struct{}], error) {
			calls++
			return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, retry.Retryable(cause)
		})
		_, result, err := execution12.Value, execution12.Retry, executionError12

		var canceled *retry.ContextError
		if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || calls != 0 || result.Reason != retry.ReasonCanceled {
			t.Fatalf("calls=%d error=%v result=%+v", calls, err, result)
		}
	})

	t.Run("during sleep", func(t *testing.T) {
		clock := newManualClock(time.Unix(100, 0))
		policy := mustPolicy(t, retry.Config{
			Backoff: retry.Constant(2 * time.Second), MaxAttempts: 2,
			Clock: clock, Sleeper: failingSleeper{context.Canceled},
			Classifier: retry.RetryableClassifier(),
		})
		execution13, executionError13 := retry.Do(context.Background(), policy, alwaysRetryable)
		_, result, err := execution13.Value, execution13.Retry, executionError13
		var canceled *retry.ContextError
		if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || result.Attempts != 1 {
			t.Fatalf("error=%v result=%+v", err, result)
		}
	})

	t.Run("after sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := newManualClock(time.Unix(100, 0))
		calls := 0
		policy := mustPolicy(t, retry.Config{
			Backoff: retry.Constant(2 * time.Second), MaxAttempts: 2,
			Clock: clock, Sleeper: cancelingSleeper{cancel: cancel},
			Classifier: retry.RetryableClassifier(),
		})
		execution14, executionError14 := retry.Do(ctx, policy, func(context.Context) (retry.AttemptResult[struct{}], error) {
			calls++
			return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, retry.Retryable(cause)
		})
		_, result, err := execution14.Value, execution14.Retry, executionError14

		var canceled *retry.ContextError
		if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || calls != 1 || result.Reason != retry.ReasonCanceled {
			t.Fatalf("calls=%d error=%v result=%+v", calls, err, result)
		}
	})

	t.Run("during successful attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := newManualClock(time.Unix(100, 0))
		policy := mustPolicy(t, baseConfig(clock, retry.Config{}))
		execution15, executionError15 := retry.Do(ctx, policy, func(context.Context) (retry.AttemptResult[struct{}], error) {
			cancel()
			return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, nil
		})
		_, result, err := execution15.Value, execution15.Retry, executionError15

		if err != nil || result.Reason != retry.ReasonSucceeded || result.Attempts != 1 {
			t.Fatalf("error=%v result=%+v", err, result)
		}
	})

	t.Run("known permanent failure wins caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := immediateTimeoutClock{newManualClock(time.Unix(100, 0))}
		policy := mustPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 2, AttemptTimeout: time.Second,
			Clock: clock, Sleeper: advancingSleeper{clock.manualClock},
			Classifier: retry.RetryableClassifier(),
		})
		execution16, executionError16 := retry.Do(ctx, policy, func(context.Context) (retry.AttemptResult[struct{}], error) {
			cancel()
			return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, cause
		})
		_, result, err := execution16.Value, execution16.Retry, executionError16

		var permanent *retry.PermanentError
		if !errors.As(err, &permanent) || !errors.Is(err, cause) || result.Reason != retry.ReasonPermanent || result.Attempts != 1 {
			t.Fatalf("error=%v result=%+v", err, result)
		}
	})
}

func TestAttemptTimeoutReturnsBudgetError(t *testing.T) {
	t.Parallel()

	clock := immediateTimeoutClock{newManualClock(time.Unix(100, 0))}
	policy := mustPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2, AttemptTimeout: time.Second,
		Clock: clock, Sleeper: advancingSleeper{clock.manualClock},
		Classifier: retry.RetryableClassifier(),
	})
	execution17, executionError17 := retry.Do(context.Background(), policy, func(ctx context.Context) (retry.AttemptResult[struct{}], error) {
		return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, ctx.Err()
	})
	_, result, err := execution17.Value, execution17.Retry, executionError17

	var budget *retry.BudgetError
	if !errors.As(err, &budget) || budget.Kind != retry.BudgetAttempt || !errors.Is(err, context.DeadlineExceeded) || result.Reason != retry.ReasonAttemptBudget {
		t.Fatalf("error=%v result=%+v", err, result)
	}
}

func TestElapsedDeadlinePrecedesLongerAttemptTimeout(t *testing.T) {
	t.Parallel()

	clock := immediateTimeoutClock{newManualClock(time.Unix(100, 0))}
	policy := mustPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2,
		MaxElapsed: time.Second, AttemptTimeout: 2 * time.Second,
		Clock: clock, Sleeper: advancingSleeper{clock.manualClock},
		Classifier: retry.RetryableClassifier(),
	})
	execution18, executionError18 := retry.Do(context.Background(), policy, func(ctx context.Context) (retry.AttemptResult[struct{}], error) {
		return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, ctx.Err()
	})
	_, result, err := execution18.Value, execution18.Retry, executionError18

	var budget *retry.BudgetError
	if !errors.As(err, &budget) || budget.Kind != retry.BudgetElapsed || result.Reason != retry.ReasonElapsedBudget {
		t.Fatalf("error=%v result=%+v", err, result)
	}
}

func TestNonContextSleeperFailureIsPermanent(t *testing.T) {
	t.Parallel()

	want := errors.New("timer backend failed")
	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, retry.Config{
		Backoff: retry.Constant(time.Second), MaxAttempts: 2,
		Clock: clock, Sleeper: failingSleeper{want},
		Classifier: retry.RetryableClassifier(),
	})
	execution19, executionError19 := retry.Do(context.Background(), policy, alwaysRetryable)
	_, result, err := execution19.Value, execution19.Retry, executionError19
	var permanent *retry.PermanentError
	if !errors.As(err, &permanent) || !errors.Is(err, want) || result.Reason != retry.ReasonSleeperFailure {
		t.Fatalf("error=%v result=%+v", err, result)
	}
}

func TestExtensionFailuresStopSafely(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		classifier retry.Classifier
	}{
		{"error", retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return 0, errors.New("classifier failed") })},
		{"invalid decision", retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return 99, nil })},
		{"panic", retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { panic("classifier panic") })},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newManualClock(time.Unix(100, 0))
			policy := mustPolicy(t, retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 2, Clock: clock,
				Sleeper: advancingSleeper{clock}, Classifier: test.classifier,
			})
			execution20, executionError20 := retry.Do(context.Background(), policy, alwaysRetryable)
			_, result, err := execution20.Value, execution20.Retry, executionError20
			var permanent *retry.PermanentError
			if (test.name == "panic" && !errors.Is(err, retry.ErrInvalidPolicy)) || (test.name != "panic" && !errors.As(err, &permanent)) || result.Attempts != 1 || result.Reason != retry.ReasonClassifierFailure {
				t.Fatalf("error=%v result=%+v", err, result)
			}
		})
	}

	t.Run("observer panic", func(t *testing.T) {
		clock := newManualClock(time.Unix(100, 0))
		config := baseConfig(clock, retry.Config{})
		config.Observer = retry.ObserveFunc(func(retry.Observation) { panic("observer panic") })
		policy := mustPolicy(t, config)
		execution21, executionError21 := retry.Do(context.Background(), policy, alwaysRetryable)
		_, result, err := execution21.Value, execution21.Retry, executionError21
		var exhausted *retry.ExhaustedError
		if !errors.As(err, &exhausted) || result.Attempts != config.MaxAttempts {
			t.Fatalf("error=%v result=%+v", err, result)
		}
	})
}

func TestOperationPanicIsNotRetried(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, baseConfig(clock, retry.Config{}))
	calls := 0
	defer func() {
		if recovered := recover(); recovered != "operation panic" || calls != 1 {
			t.Fatalf("recovered=%v calls=%d", recovered, calls)
		}
	}()
	execution22, executionError22 := retry.Do(context.Background(), policy, func(context.Context) (retry.AttemptResult[struct{}], error) {
		calls++
		panic("operation panic")
	})
	_, _, _ = execution22.Value, execution22.Retry, executionError22

}

func baseConfig(clock *manualClock, override retry.Config) retry.Config {
	config := retry.Config{
		Backoff: retry.Constant(2 * time.Second), MaxAttempts: 3,
		Clock: clock, Sleeper: advancingSleeper{clock},
		Classifier: retry.RetryableClassifier(), HistoryLimit: 3,
	}
	if override.MaxElapsed != 0 {
		config.MaxElapsed = override.MaxElapsed
	}
	if override.MaxSleep != 0 {
		config.MaxSleep = override.MaxSleep
	}
	return config
}

func alwaysRetryable(context.Context) (retry.AttemptResult[struct{}], error) {
	return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
}

type failingSleeper struct{ err error }

func (sleeper failingSleeper) Sleep(context.Context, time.Duration) error { return sleeper.err }

type cancelingSleeper struct{ cancel context.CancelFunc }

func (sleeper cancelingSleeper) Sleep(context.Context, time.Duration) error {
	sleeper.cancel()
	return nil
}

type immediateTimeoutClock struct{ *manualClock }

func (clock immediateTimeoutClock) WithTimeout(parent context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, time.Unix(0, 0))
}

func TestStopBeforeDeadlineReturnsTheLastFailureInsteadOfSleepingPastIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		deadline time.Duration
		adjust   func(*retry.Config)
		attempts uint
		reason   retry.Reason
		slept    time.Duration
	}{
		{"delay passes the deadline", true, 3 * time.Second, nil, 2, retry.ReasonDeadlineBudget, 2 * time.Second},
		{"delay ends exactly at the deadline", true, 4 * time.Second, nil, 2, retry.ReasonDeadlineBudget, 2 * time.Second},
		{"delay ends before the deadline", true, 4*time.Second + time.Nanosecond, nil, 3, retry.ReasonAttemptsExhausted, 4 * time.Second},
		{"minimum delay passes the deadline", true, 3 * time.Second, func(config *retry.Config) { config.MinDelay = 5 * time.Second }, 1, retry.ReasonDeadlineBudget, 0},
		{"zero delay before the deadline", true, time.Second, func(config *retry.Config) { config.Backoff = retry.Constant(0) }, 3, retry.ReasonAttemptsExhausted, 0},
		{"elapsed budget wins", true, time.Second, func(config *retry.Config) { config.MaxElapsed = time.Second }, 1, retry.ReasonElapsedBudget, 0},
		{"sleep budget wins", true, time.Second, func(config *retry.Config) { config.MaxSleep = time.Second }, 1, retry.ReasonSleepBudget, 0},
		{"disabled", false, 3 * time.Second, nil, 3, retry.ReasonAttemptsExhausted, 4 * time.Second},
		{"no deadline", true, 0, nil, 3, retry.ReasonAttemptsExhausted, 4 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// The clock starts an hour ahead so contexts built from it cannot
			// expire in real time while the test runs.
			start := time.Now().Add(time.Hour)
			clock := newManualClock(start)
			config := baseConfig(clock, retry.Config{})
			config.StopBeforeDeadline = test.enabled
			if test.adjust != nil {
				test.adjust(&config)
			}
			ctx := context.Background()
			if test.deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, start.Add(test.deadline))
				defer cancel()
			}
			execution, err := retry.Do(ctx, mustPolicy(t, config), alwaysRetryable)
			if execution.Retry.Attempts != test.attempts || execution.Retry.Reason != test.reason {
				t.Fatalf("result = %+v, want %d attempts and %s", execution.Retry, test.attempts, test.reason)
			}
			var budget *retry.BudgetError
			if isDeadline := errors.As(err, &budget) && budget.Kind == retry.BudgetDeadline; isDeadline != (test.reason == retry.ReasonDeadlineBudget) {
				t.Fatalf("error = %v", err)
			}
			if test.reason == retry.ReasonDeadlineBudget && err.Error() != "retry deadline budget exhausted" {
				t.Fatalf("error = %q", err)
			}
			if slept := clock.Now().Sub(start); slept != test.slept {
				t.Fatalf("slept %s, want %s", slept, test.slept)
			}
		})
	}
}
