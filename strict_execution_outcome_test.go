package retry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
)

func TestDoStrictRejectsInvalidInputsBeforeDispatch(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
	var typedNilContext *nilContext
	operation := retry.StrictOperation[string](func(context.Context) (retry.AttemptResult[string], error) {
		t.Fatal("operation was dispatched")
		return retry.AttemptResult[string]{}, nil
	})

	tests := []struct {
		name      string
		ctx       context.Context
		policy    *retry.Policy
		operation retry.StrictOperation[string]
	}{
		{"nil context", nil, policy, operation},
		{"typed nil context", typedNilContext, policy, operation},
		{"nil policy", context.Background(), nil, operation},
		{"nil operation", context.Background(), policy, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := retry.DoStrict(test.ctx, test.policy, test.operation)
			const want = "invalid retry policy: context, policy, and operation are required"
			if result.Value != "" || result.Outcome != retry.OutcomeNotDispatched || result.Retry.Attempts != 0 || result.Retry.Reason != "" || len(result.Retry.History) != 0 || err == nil || err.Error() != want || len(err.Error()) != 65 || len(err.Error()) > retry.MaxStrictTerminalErrorBytes || !errors.Is(err, retry.ErrInvalidPolicy) {
				t.Fatalf("DoStrict = (%+v, %v), want zero and %q", result, err, want)
			}
		})
	}
}

func TestDoStrictReportsPredispatchContextOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"canceled", canceledContext(), context.Canceled},
		{"deadline", expiredContext(), context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newManualClock(time.Unix(100, 0))
			policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
			calls := 0
			result, err := retry.DoStrict(test.ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
				calls++
				return retry.AttemptResult[string]{Value: "unexpected", Outcome: retry.OutcomeKnown}, nil
			})
			var contextErr *retry.ContextError
			if calls != 0 || !errors.As(err, &contextErr) || !errors.Is(err, test.want) || errors.Is(err, otherContextSentinel(test.want)) || errors.Is(err, retry.ErrOutcomeUnknown) {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
			}
			if result.Outcome != retry.OutcomeNotDispatched || result.Value != "" || result.Retry.Reason != retry.ReasonCanceled || contextErr.Outcome() != retry.OutcomeNotDispatched {
				t.Fatalf("result=%+v context=%+v", result, contextErr)
			}
			wantText := "retry canceled"
			if errors.Is(test.want, context.DeadlineExceeded) {
				wantText = "retry deadline exceeded"
			}
			//nolint:errorlint // ContextError unwraps the exact normalized sentinel.
			if err.Error() != wantText || contextErr.Unwrap() != test.want || !equalRetryResult(contextErr.Result(), result.Retry) {
				t.Fatalf("error=%q unwrap=%v", err, contextErr.Unwrap())
			}
		})
	}
}

func TestDoStrictDoesNotDiscloseCustomCancellationCause(t *testing.T) {
	t.Parallel()

	sensitive := errors.New("customer secret")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(sensitive)
	clock := newManualClock(time.Unix(100, 0))
	policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		t.Fatal("operation dispatched")
		return retry.AttemptResult[string]{}, nil
	})
	if err == nil || err.Error() != "retry canceled" || !errors.Is(err, context.Canceled) || errors.Is(err, sensitive) || result.Outcome != retry.OutcomeNotDispatched {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoStrictKnownResultWinsCancellationRace(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	clock := newManualClock(time.Unix(100, 0))
	policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		cancel()
		return retry.AttemptResult[string]{Value: "accepted", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "accepted" || result.Outcome != retry.OutcomeKnown || result.Retry.Attempts != 1 || result.Retry.Reason != retry.ReasonSucceeded {
		t.Fatalf("DoStrict = (%+v, %v)", result, err)
	}
}

func TestDoStrictUnknownOutcomeIsBoundedAndNeverRetried(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		callback     error
		cancelDuring bool
		wantNested   error
	}{
		{"caller canceled", errors.New("secret callback"), true, context.Canceled},
		{"callback canceled", fmt.Errorf("secret: %w", context.Canceled), false, context.Canceled},
		{"callback deadline", fmt.Errorf("secret: %w", context.DeadlineExceeded), false, context.DeadlineExceeded},
		{"callback matches both", errors.Join(context.Canceled, context.DeadlineExceeded), false, context.Canceled},
		{"unclassified", errors.New("secret callback"), false, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			cancel := func() {}
			if test.cancelDuring {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			clock := newManualClock(time.Unix(100, 0))
			observations := make([]retry.Observation, 0, 1)
			config := baseConfig(clock, retry.Config{HistoryLimit: 1})
			config.Observer = retry.ObserveFunc(func(observation retry.Observation) { observations = append(observations, observation) })
			policy := mustStrictPolicy(t, config)
			calls := 0
			result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
				calls++
				if test.cancelDuring {
					cancel()
				}
				return retry.AttemptResult[string]{Value: "secret value", Outcome: retry.OutcomeUnknown}, test.callback
			})
			if calls != 1 || err == nil || err.Error() != "retry outcome unknown" || !errors.Is(err, retry.ErrOutcomeUnknown) {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
			}
			if result.Value != "" || result.Outcome != retry.OutcomeUnknown || result.Retry.Attempts != 1 || result.Retry.Reason != retry.ReasonOutcomeUnknown {
				t.Fatalf("result=%+v", result)
			}
			if len(result.Retry.History) != 1 || result.Retry.History[0].Attempt != 1 || result.Retry.History[0].Err != nil || result.Retry.History[0].Classification != 0 || result.Retry.History[0].Delay != 0 {
				t.Fatalf("history=%+v", result.Retry.History)
			}
			if len(observations) != 1 || observations[0].Attempt != 1 || observations[0].Reason != retry.ReasonOutcomeUnknown {
				t.Fatalf("observations=%+v", observations)
			}
			if test.wantNested == nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, test.callback) {
					t.Fatalf("unexpected retained cause: %v", err)
				}
			} else if !errors.Is(err, test.wantNested) || errors.Is(err, otherContextSentinel(test.wantNested)) || errors.Is(err, test.callback) {
				t.Fatalf("nested category mismatch: %v", err)
			}
		})
	}
}

func TestDoStrictRejectsInvalidPostDispatchOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		result retry.AttemptResult[string]
		err    error
	}{
		{retry.AttemptResult[string]{Outcome: retry.OutcomeUnknown}, nil},
		{retry.AttemptResult[string]{Outcome: retry.OutcomeNotDispatched}, nil},
		{retry.AttemptResult[string]{Outcome: retry.OutcomeNotDispatched}, errors.New("secret callback")},
		{retry.AttemptResult[string]{Outcome: retry.Outcome(99)}, nil},
		{retry.AttemptResult[string]{Outcome: retry.Outcome(99)}, errors.New("secret callback")},
	}
	for _, test := range tests {
		clock := newManualClock(time.Unix(100, 0))
		ctx, cancel := context.WithCancel(context.Background())
		observations := make([]retry.Observation, 0, 1)
		config := baseConfig(clock, retry.Config{HistoryLimit: 1})
		config.Observer = retry.ObserveFunc(func(observation retry.Observation) { observations = append(observations, observation) })
		policy := mustStrictPolicy(t, config)
		result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			cancel()
			return test.result, test.err
		})
		cancel()
		const want = "invalid retry policy: strict operation returned an invalid outcome"
		var contextErr *retry.ContextError
		if err == nil || err.Error() != want || len(err.Error()) != retry.MaxStrictTerminalErrorBytes || !errors.Is(err, retry.ErrInvalidPolicy) || errors.Is(err, retry.ErrOutcomeUnknown) || errors.As(err, &contextErr) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if result.Outcome != retry.OutcomeUnknown || result.Value != "" || result.Retry.Attempts != 1 || result.Retry.Reason != retry.ReasonOutcomeUnknown {
			t.Fatalf("result=%+v", result)
		}
		if len(result.Retry.History) != 1 || result.Retry.History[0].Attempt != 1 || result.Retry.History[0].Classification != 0 || result.Retry.History[0].Err != nil || result.Retry.History[0].Delay != 0 {
			t.Fatalf("history=%+v", result.Retry.History)
		}
		if len(observations) != 1 || observations[0].Attempt != 1 || observations[0].Reason != retry.ReasonOutcomeUnknown {
			t.Fatalf("observations=%+v", observations)
		}
	}
}

func TestDoStrictCallerDeadlineAfterDispatchIsUnknown(t *testing.T) {
	t.Parallel()

	ctx := &sequencedOutcomeContext{errAt: 3, err: context.DeadlineExceeded}
	clock := newManualClock(time.Unix(100, 0))
	policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Outcome: retry.OutcomeUnknown}, errors.New("ambiguous")
	})
	var contextErr *retry.ContextError
	if !errors.As(err, &contextErr) || err.Error() != "retry outcome unknown" || !errors.Is(err, retry.ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || contextErr.Outcome() != retry.OutcomeUnknown || result.Outcome != retry.OutcomeUnknown {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoStrictKnownFailureWinsCallerDeadlineRace(t *testing.T) {
	t.Parallel()

	ctx := &mutableStrictContext{}
	clock := newManualClock(time.Unix(175, 0))
	operationErr := errors.New("known permanent failure")
	config := baseConfig(clock, retry.Config{})
	config.Classifier = retry.ClassifyFunc(func(got context.Context, err error) (retry.Classification, error) {
		//nolint:errorlint // The classifier must receive the exact borrowed operation error.
		if got != ctx || err != operationErr {
			t.Fatalf("classifier ctx=%v err=%v", got, err)
		}
		return retry.ClassificationPermanent, nil
	})
	policy := mustStrictPolicy(t, config)
	result, err := retry.DoStrict(ctx, policy, func(got context.Context) (retry.AttemptResult[string], error) {
		if got != ctx {
			t.Fatal("operation context identity changed")
		}
		ctx.err = context.DeadlineExceeded
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, operationErr
	})
	var permanent *retry.PermanentError
	var budget *retry.BudgetError
	if !errors.As(err, &permanent) || errors.As(err, &budget) || !errors.Is(err, operationErr) || errors.Is(err, context.DeadlineExceeded) || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonPermanent {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoStrictUnknownAndInvalidOutcomesKeepHistoryDisabled(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	policy := mustStrictPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
	})
	for _, operation := range []retry.StrictOperation[string]{
		func(context.Context) (retry.AttemptResult[string], error) {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeUnknown}, errors.New("ambiguous")
		},
		func(context.Context) (retry.AttemptResult[string], error) {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeNotDispatched}, errors.New("invalid")
		},
	} {
		result, err := retry.DoStrict(context.Background(), policy, operation)
		if err == nil || len(result.Retry.History) != 0 || result.Retry.Attempts != 1 || result.Outcome != retry.OutcomeUnknown {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func otherContextSentinel(value error) error {
	if errors.Is(value, context.Canceled) {
		return context.DeadlineExceeded
	}
	return context.Canceled
}

func equalRetryResult(left, right retry.Result) bool {
	if left.Attempts != right.Attempts || left.Elapsed != right.Elapsed || left.FinalDelay != right.FinalDelay || left.Reason != right.Reason || len(left.History) != len(right.History) {
		return false
	}
	for index := range left.History {
		if left.History[index] != right.History[index] {
			return false
		}
	}
	return true
}

type sequencedOutcomeContext struct {
	calls int
	errAt int
	err   error
}

func (*sequencedOutcomeContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*sequencedOutcomeContext) Done() <-chan struct{}       { return nil }
func (ctx *sequencedOutcomeContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.errAt {
		return ctx.err
	}
	return nil
}
func (*sequencedOutcomeContext) Value(any) any { return nil }

func mustStrictPolicy(t *testing.T, config retry.Config) *retry.Policy {
	t.Helper()
	policy, err := retry.NewPolicyStrict(config)
	if err != nil {
		t.Fatalf("NewPolicyStrict: %v", err)
	}
	return policy
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func expiredContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	return ctx
}
