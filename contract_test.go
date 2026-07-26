package retry_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func TestBackoffHostileInputsRemainBounded(t *testing.T) {
	t.Parallel()

	if got := retry.Constant(-time.Second).Delay(1, 0, nil); got != 0 {
		t.Fatalf("negative constant = %s", got)
	}
	if got := retry.Linear(time.Second, time.Second).Delay(0, 0, nil); got != time.Second {
		t.Fatalf("linear attempt zero = %s", got)
	}
	if got := retry.Polynomial(-time.Second, -time.Second, 2).Delay(2, 0, nil); got != 0 {
		t.Fatalf("negative polynomial = %s", got)
	}
	if got := retry.Exponential(time.Second, 2).Delay(0, 0, nil); got != time.Second {
		t.Fatalf("exponential attempt zero = %s", got)
	}
	if got := retry.FullJitter(nil).Delay(1, 0, fixedRandom(1)); got != 0 {
		t.Fatalf("nil full jitter = %s", got)
	}
	if got := retry.EqualJitter(nil).Delay(1, 0, fixedRandom(1)); got != 0 {
		t.Fatalf("nil equal jitter = %s", got)
	}
	if got := retry.ExponentialJitter(time.Second, 2, -1).Delay(1, 0, nil); got != time.Second {
		t.Fatalf("negative jitter factor = %s", got)
	}
	if got := retry.ExponentialJitter(time.Second, 2, 2).Delay(1, 0, nil); got != 0 {
		t.Fatalf("oversized jitter factor lower bound = %s", got)
	}
	if got := retry.FullJitter(retry.Constant(time.Second)).Delay(1, 0, hostileRandom{}); got < 0 || got > time.Second {
		t.Fatalf("hostile random produced %s", got)
	}
	if got := retry.FullJitter(retry.Constant(time.Second)).Delay(1, 0, negativeRandom{}); got != time.Nanosecond {
		t.Fatalf("negative random produced %s", got)
	}
	if got := retry.Exponential(time.Duration(math.MaxInt64), 0).Delay(2, 0, nil); got != 0 {
		t.Fatalf("zero multiplier = %s", got)
	}
}

func TestTypedMarkerAndTerminalErrorContracts(t *testing.T) {
	t.Parallel()

	if retry.Retryable(nil) != nil || retry.Permanent(nil) != nil {
		t.Fatal("nil marker did not remain nil")
	}
	cause := errors.New("cause")
	marked := retry.Permanent(cause)
	var permanent *retry.PermanentError
	if !errors.As(marked, &permanent) || !errors.Is(marked, cause) || !strings.Contains(marked.Error(), "permanent") {
		t.Fatalf("permanent marker = %v", marked)
	}
	classification, err := retry.RetryableClassifier().Classify(context.Background(), marked)
	if err != nil || classification != retry.ClassificationPermanent {
		t.Fatalf("permanent classification = (%v, %v)", classification, err)
	}

	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, baseConfig(clock, retry.Config{}))
	_, exhaustedResult, exhaustedErr := retry.Do(context.Background(), policy, alwaysRetryable)
	if !strings.Contains(exhaustedErr.Error(), "exhausted") {
		t.Fatalf("exhausted error = %v", exhaustedErr)
	}
	var exhausted *retry.ExhaustedError
	if !errors.As(exhaustedErr, &exhausted) || exhausted.Result().Reason != exhaustedResult.Reason {
		t.Fatalf("exhausted result mismatch")
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, canceledResult, canceledErr := retry.Do(canceledContext, policy, alwaysRetryable)
	var canceled *retry.CanceledError
	if !errors.As(canceledErr, &canceled) || canceled.Result().Reason != canceledResult.Reason || !strings.Contains(canceledErr.Error(), "canceled") {
		t.Fatalf("canceled contract = %v", canceledErr)
	}

	budgetPolicy := mustPolicy(t, baseConfig(clock, retry.Config{MaxSleep: time.Nanosecond}))
	_, budgetResult, budgetErr := retry.Do(context.Background(), budgetPolicy, alwaysRetryable)
	var budget *retry.BudgetError
	if !errors.As(budgetErr, &budget) || budget.Result().Reason != budgetResult.Reason || !strings.Contains(budgetErr.Error(), "sleep") {
		t.Fatalf("budget contract = %v", budgetErr)
	}
}

func TestPolicyValidationMatrix(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	valid := retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
	}
	tests := make([]retry.Config, 0, 10)
	tests = append(tests,
		retry.Config{MaxAttempts: 1, Clock: clock, Sleeper: valid.Sleeper, Classifier: valid.Classifier},
		retry.Config{Backoff: valid.Backoff, MaxAttempts: 1, Sleeper: valid.Sleeper, Classifier: valid.Classifier},
		retry.Config{Backoff: valid.Backoff, MaxAttempts: 1, Clock: clock, Classifier: valid.Classifier},
		retry.Config{Backoff: valid.Backoff, MaxAttempts: 1, Clock: clock, Sleeper: valid.Sleeper},
		withDurations(valid, -1, 0, 0, 0, 0),
		withDurations(valid, 0, -1, 0, 0, 0),
		withDurations(valid, 0, 0, -1, 0, 0),
		withDurations(valid, 0, 0, 0, -1, 0),
		withDurations(valid, 0, 0, 0, 0, -1),
	)
	history := valid
	history.HistoryLimit = retry.MaxHistoryEntries + 1
	tests = append(tests, history)
	for index, config := range tests {
		if _, err := retry.NewPolicy(config); !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Errorf("case %d error = %v", index, err)
		}
	}
}

func TestPolicyRejectsTypedNilRequiredDependencies(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	var backoff *nilBackoff
	var classifier *nilClassifier
	var sleeper *nilSleeper
	var typedClock *nilClock
	tests := []retry.Config{
		{Backoff: backoff, MaxAttempts: 1, Clock: clock, Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier()},
		{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: typedClock, Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier()},
		{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock, Sleeper: sleeper, Classifier: retry.RetryableClassifier()},
		{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock, Sleeper: advancingSleeper{clock}, Classifier: classifier},
	}
	for index, config := range tests {
		if _, err := retry.NewPolicy(config); !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Errorf("case %d error = %v", index, err)
		}
	}
}

func TestExecutionBoundaryPaths(t *testing.T) {
	t.Parallel()

	if _, _, err := retry.Do[struct{}](context.Background(), nil, alwaysRetryable); !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("nil policy error = %v", err)
	}
	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, baseConfig(clock, retry.Config{}))
	if _, _, err := retry.Do[struct{}](context.Background(), policy, nil); !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("nil operation error = %v", err)
	}
	cause := errors.New("do not retry")
	_, result, err := retry.Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		return struct{}{}, retry.Permanent(cause)
	})
	var permanent *retry.PermanentError
	if !errors.As(err, &permanent) || !errors.Is(err, cause) || result.Reason != retry.ReasonPermanent {
		t.Fatalf("permanent execution = %v %+v", err, result)
	}

	stepping := &sequenceClock{times: []time.Time{time.Unix(100, 0), time.Unix(102, 0), time.Unix(102, 0)}}
	elapsedPolicy := mustPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2, MaxElapsed: time.Second,
		Clock: stepping, Sleeper: failingSleeper{errors.New("unused")},
		Classifier: retry.RetryableClassifier(),
	})
	calls := 0
	_, elapsedResult, elapsedErr := retry.Do(context.Background(), elapsedPolicy, func(context.Context) (struct{}, error) {
		calls++
		return struct{}{}, nil
	})
	var budget *retry.BudgetError
	if !errors.As(elapsedErr, &budget) || calls != 0 || elapsedResult.Reason != retry.ReasonElapsedBudget {
		t.Fatalf("pre-attempt elapsed = %v %+v calls=%d", elapsedErr, elapsedResult, calls)
	}
}

func TestHistoryRetainsLatestFailuresWithinBound(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	config := baseConfig(clock, retry.Config{})
	config.MaxAttempts = 4
	config.HistoryLimit = 2
	policy := mustPolicy(t, config)
	_, result, _ := retry.Do(context.Background(), policy, alwaysRetryable)
	if len(result.History) != 2 || result.History[0].Attempt != 3 || result.History[1].Attempt != 4 {
		t.Fatalf("history = %+v", result.History)
	}
}

func TestDelayHintRaisesTheSelectedDelay(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, retry.Config{
		Backoff: retry.Constant(time.Second), MaxAttempts: 2,
		Clock: clock, Sleeper: advancingSleeper{clock},
		Classifier: retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) {
			return retry.ClassificationRetryable, nil
		}),
	})
	calls := 0
	_, result, err := retry.Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		calls++
		if calls == 1 {
			return struct{}{}, hintedError{delay: 3 * time.Second}
		}
		return struct{}{}, nil
	})
	if err != nil || result.FinalDelay != 3*time.Second || result.Elapsed != 3*time.Second {
		t.Fatalf("error=%v result=%+v", err, result)
	}
}

func withDurations(config retry.Config, elapsed, attempt, minimum, maximum, sleep time.Duration) retry.Config {
	config.MaxElapsed = elapsed
	config.AttemptTimeout = attempt
	config.MinDelay = minimum
	config.MaxDelay = maximum
	config.MaxSleep = sleep
	return config
}

type hostileRandom struct{}

func (hostileRandom) Int64n(int64) int64 { return math.MaxInt64 }

type negativeRandom struct{}

func (negativeRandom) Int64n(int64) int64 { return -1 }

type hintedError struct{ delay time.Duration }

func (err hintedError) Error() string { return "retry later" }

func (err hintedError) RetryDelay(time.Time) (time.Duration, bool) { return err.delay, true }

type sequenceClock struct {
	mu    sync.Mutex
	times []time.Time
}

type nilBackoff struct{}

func (*nilBackoff) Delay(uint, time.Duration, retry.Random) time.Duration { return 0 }

type nilClassifier struct{}

func (*nilClassifier) Classify(context.Context, error) (retry.Classification, error) {
	return retry.ClassificationPermanent, nil
}

type nilSleeper struct{}

func (*nilSleeper) Sleep(context.Context, time.Duration) error { return nil }

type nilClock struct{}

func (*nilClock) Now() time.Time { return time.Time{} }

func (clock *sequenceClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if len(clock.times) == 1 {
		return clock.times[0]
	}
	now := clock.times[0]
	clock.times = clock.times[1:]
	return now
}
