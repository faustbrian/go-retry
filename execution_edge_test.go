package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
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
			_, result, err := retry.Do(context.Background(), policy, alwaysRetryable)
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
			_, result, _ := retry.Do(context.Background(), policy, alwaysRetryable)
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
		_, result, err := retry.Do(ctx, policy, func(context.Context) (struct{}, error) {
			calls++
			return struct{}{}, retry.Retryable(cause)
		})
		var canceled *retry.CanceledError
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
		_, result, err := retry.Do(context.Background(), policy, alwaysRetryable)
		var canceled *retry.CanceledError
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
		_, result, err := retry.Do(ctx, policy, func(context.Context) (struct{}, error) {
			calls++
			return struct{}{}, retry.Retryable(cause)
		})
		var canceled *retry.CanceledError
		if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || calls != 1 || result.Reason != retry.ReasonCanceled {
			t.Fatalf("calls=%d error=%v result=%+v", calls, err, result)
		}
	})

	t.Run("during successful attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := newManualClock(time.Unix(100, 0))
		policy := mustPolicy(t, baseConfig(clock, retry.Config{}))
		_, result, err := retry.Do(ctx, policy, func(context.Context) (struct{}, error) {
			cancel()
			return struct{}{}, nil
		})
		var canceled *retry.CanceledError
		if !errors.As(err, &canceled) || !errors.Is(err, context.Canceled) || result.Reason != retry.ReasonCanceled {
			t.Fatalf("error=%v result=%+v", err, result)
		}
	})

	t.Run("caller deadline beats attempt timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := immediateTimeoutClock{newManualClock(time.Unix(100, 0))}
		policy := mustPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 2, AttemptTimeout: time.Second,
			Clock: clock, Sleeper: advancingSleeper{clock.manualClock},
			Classifier: retry.RetryableClassifier(),
		})
		_, result, err := retry.Do(ctx, policy, func(context.Context) (struct{}, error) {
			cancel()
			return struct{}{}, cause
		})
		var canceled *retry.CanceledError
		if !errors.As(err, &canceled) || result.Reason != retry.ReasonCanceled {
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
	_, result, err := retry.Do(context.Background(), policy, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, ctx.Err()
	})
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
	_, result, err := retry.Do(context.Background(), policy, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, ctx.Err()
	})
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
	_, result, err := retry.Do(context.Background(), policy, alwaysRetryable)
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
			_, result, err := retry.Do(context.Background(), policy, alwaysRetryable)
			var permanent *retry.PermanentError
			if !errors.As(err, &permanent) || result.Attempts != 1 || result.Reason != retry.ReasonClassifierFailure {
				t.Fatalf("error=%v result=%+v", err, result)
			}
		})
	}

	t.Run("observer panic", func(t *testing.T) {
		clock := newManualClock(time.Unix(100, 0))
		config := baseConfig(clock, retry.Config{})
		config.Observer = retry.ObserveFunc(func(retry.Observation) { panic("observer panic") })
		policy := mustPolicy(t, config)
		_, result, err := retry.Do(context.Background(), policy, alwaysRetryable)
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
	_, _, _ = retry.Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		calls++
		panic("operation panic")
	})
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

func alwaysRetryable(context.Context) (struct{}, error) {
	return struct{}{}, retry.Retryable(errors.New("temporary"))
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
