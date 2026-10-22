package retry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v3"
)

func TestDoRetriesExplicitFailuresAndReturnsMetadata(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, retry.Config{
		Backoff:     retry.Constant(2 * time.Second),
		MaxAttempts: 3,
		Clock:       clock,
		Sleeper:     advancingSleeper{clock: clock},
		Classifier: retry.ClassifyFunc(func(_ context.Context, _ error) (retry.Classification, error) {
			return retry.ClassificationRetryable, nil
		}),
		HistoryLimit: 2,
	})

	attempts := 0
	execution33, executionError33 := retry.Do(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		attempts++
		if attempts < 3 {
			return retry.AttemptResult[string]{Value: "discarded", Outcome: retry.OutcomeKnown}, errors.New("temporary")
		}
		return retry.AttemptResult[string]{Value: "kept", Outcome: retry.OutcomeKnown}, nil
	})
	value, result, err := execution33.Value, execution33.Retry, executionError33

	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if value != "kept" {
		t.Fatalf("value = %q, want kept", value)
	}
	if result.Attempts != 3 || result.Elapsed != 4*time.Second || result.FinalDelay != 2*time.Second || result.Reason != retry.ReasonSucceeded {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.History) != 2 || result.History[0].Attempt != 1 || result.History[1].Attempt != 2 {
		t.Fatalf("unexpected bounded history: %+v", result.History)
	}
}

func TestDoReturnsTypedExhaustedErrorWithCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("still unavailable")
	clock := newManualClock(time.Unix(100, 0))
	policy := mustPolicy(t, retry.Config{
		Backoff:      retry.Constant(time.Second),
		MaxAttempts:  2,
		Clock:        clock,
		Sleeper:      advancingSleeper{clock: clock},
		Classifier:   retry.RetryableClassifier(),
		HistoryLimit: 1,
	})
	execution34, executionError34 := retry.Do(context.Background(), policy, func(context.Context) (retry.AttemptResult[struct{}], error) {
		return retry.AttemptResult[struct{}]{Value: struct{}{}, Outcome: retry.OutcomeKnown}, retry.Retryable(cause)
	})
	_, result, err := execution34.Value, execution34.Retry, executionError34

	var exhausted *retry.ExhaustedError
	if !errors.As(err, &exhausted) || !errors.Is(err, cause) {
		t.Fatalf("error = %v, want exhausted error preserving cause", err)
	}
	if result.Reason != retry.ReasonAttemptsExhausted || exhausted.Result().Attempts != 2 {
		t.Fatalf("unexpected exhausted metadata: result=%+v error=%+v", result, exhausted.Result())
	}
}

func TestPolicyRejectsUnboundedOrImplicitClassification(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	tests := []retry.Config{
		{Backoff: retry.Constant(0), Clock: clock, Sleeper: advancingSleeper{clock: clock}, Classifier: retry.RetryableClassifier()},
		{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock, Sleeper: advancingSleeper{clock: clock}},
		{Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock, Sleeper: advancingSleeper{clock: clock}, Classifier: retry.RetryableClassifier(), MinDelay: 2 * time.Second, MaxDelay: time.Second},
	}

	for index, config := range tests {
		if _, err := retry.NewPolicy(config); !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("case %d: got %v, want ErrInvalidPolicy", index, err)
		}
	}
}

func mustPolicy(t *testing.T, config retry.Config) *retry.Policy {
	t.Helper()
	policy, err := retry.NewPolicy(config)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return policy
}

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func newManualClock(now time.Time) *manualClock { return &manualClock{now: now} }

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type advancingSleeper struct{ clock *manualClock }

func (sleeper advancingSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sleeper.clock.advance(duration)
	return nil
}
