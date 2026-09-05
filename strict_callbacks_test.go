package retry_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/faustbrian/go-resilience"
	retry "github.com/faustbrian/go-retry"
)

func TestDoStrictNormalizesContextMatchingSleeperErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
		text string
	}{
		{"canceled", fmt.Errorf("sensitive sleeper: %w", context.Canceled), context.Canceled, "retry canceled"},
		{"deadline", fmt.Errorf("sensitive sleeper: %w", context.DeadlineExceeded), context.DeadlineExceeded, "retry deadline exceeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newManualClock(time.Unix(100, 0))
			config := baseConfig(clock, retry.Config{})
			config.Sleeper = failingSleeper{test.err}
			policy := mustStrictPolicy(t, config)
			result, err := retry.DoStrict(context.Background(), policy, strictRetryableFailure)
			var contextErr *retry.ContextError
			if !errors.As(err, &contextErr) || err.Error() != test.text || !errors.Is(err, test.want) || errors.Is(err, test.err) || contextErr.Outcome() != retry.OutcomeKnown || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonCanceled {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestDoStrictChecksCancellationBetweenAttempts(t *testing.T) {
	t.Parallel()

	t.Run("after sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := newManualClock(time.Unix(100, 0))
		config := baseConfig(clock, retry.Config{})
		config.Sleeper = cancelingSleeper{cancel: cancel}
		policy := mustStrictPolicy(t, config)
		result, err := retry.DoStrict(ctx, policy, strictRetryableFailure)
		var contextErr *retry.ContextError
		if !errors.As(err, &contextErr) || result.Outcome != retry.OutcomeKnown || result.Retry.Attempts != 1 || result.Retry.Reason != retry.ReasonCanceled {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("before next dispatch", func(t *testing.T) {
		ctx := &sequencedErrorContext{cancelAt: 6}
		clock := newManualClock(time.Unix(100, 0))
		policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
		calls := 0
		result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
		})
		var contextErr *retry.ContextError
		if calls != 1 || !errors.As(err, &contextErr) || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonCanceled {
			t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
		}
	})

	t.Run("caller cancellation dominates sleeper error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := newManualClock(time.Unix(100, 0))
		config := baseConfig(clock, retry.Config{})
		config.Sleeper = cancelAndFailSleeper{cancel: cancel, err: errors.New("sensitive sleeper")}
		policy := mustStrictPolicy(t, config)
		result, err := retry.DoStrict(ctx, policy, strictRetryableFailure)
		if !errors.Is(err, context.Canceled) || err.Error() != "retry canceled" || result.Outcome != retry.OutcomeKnown {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
}

func TestDoStrictDeadlineBetweenAttempts(t *testing.T) {
	t.Parallel()

	ctx := &mutableStrictContext{}
	clock := newManualClock(time.Unix(101, 0))
	config := baseConfig(clock, retry.Config{})
	config.Sleeper = deadlineSettingSleeper{ctx: ctx}
	policy := mustStrictPolicy(t, config)
	result, err := retry.DoStrict(ctx, policy, strictRetryableFailure)
	var contextErr *retry.ContextError
	if !errors.As(err, &contextErr) || err.Error() != "retry deadline exceeded" || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, retry.ErrOutcomeUnknown) || contextErr.Outcome() != retry.OutcomeKnown || result.Outcome != retry.OutcomeKnown || result.Retry.Attempts != 1 || result.Retry.Reason != retry.ReasonCanceled || !equalRetryResult(contextErr.Result(), result.Retry) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoStrictChecksElapsedBudgetBeforeFirstAndLaterDispatch(t *testing.T) {
	t.Parallel()

	t.Run("before first", func(t *testing.T) {
		start := time.Unix(100, 0)
		clock := &sequenceClock{times: []time.Time{start, start.Add(2 * time.Second), start.Add(2 * time.Second)}}
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 2, MaxElapsed: time.Second,
			Clock: clock, Sleeper: failingSleeper{errors.New("unused")}, Classifier: retry.RetryableClassifier(),
		})
		calls := 0
		result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, errors.New("unused")
		})
		var budget *retry.BudgetError
		if calls != 0 || !errors.As(err, &budget) || budget.Kind != retry.BudgetElapsed || err.Error() != "retry elapsed budget exhausted: retry deadline reached" || result.Outcome != retry.OutcomeNotDispatched || result.Retry.Reason != retry.ReasonElapsedBudget || !equalRetryResult(budget.Result(), result.Retry) {
			t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
		}
		assertDefensiveStrictResult(t, budget.Result, result.Retry)
		assertStrictCarrier(t, err, []error{context.DeadlineExceeded})
	})

	t.Run("between attempts", func(t *testing.T) {
		clock := newManualClock(time.Unix(100, 0))
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 3, MaxElapsed: time.Second,
			Clock: clock, Sleeper: fixedAdvancingSleeper{clock: clock, advance: 2 * time.Second},
			Classifier: retry.RetryableClassifier(), HistoryLimit: 1,
		})
		calls := 0
		result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
		})
		var budget *retry.BudgetError
		if calls != 1 || !errors.As(err, &budget) || budget.Kind != retry.BudgetElapsed || err.Error() != "retry elapsed budget exhausted: retry deadline reached" || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonElapsedBudget || !equalRetryResult(budget.Result(), result.Retry) {
			t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
		}
		assertDefensiveStrictResult(t, budget.Result, result.Retry)
		assertStrictCarrier(t, err, []error{context.DeadlineExceeded})
	})
}

func TestDoStrictCompletesPermitsAndReportsLaterWorkBudgetFailure(t *testing.T) {
	t.Parallel()

	for _, historyLimit := range []uint{0, 2} {
		clock := newManualClock(time.Unix(500, 0))
		budgetValue, err := resilience.NewBudget(resilience.BudgetConfig{
			MaxResources: 1, MaxAdditionalPerExecution: 1,
			MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1,
			AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock,
		})
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
		if err != nil {
			t.Fatal(err)
		}
		scope, ctx, err := budgetValue.Start(context.Background(), metadata)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = scope.Close() }()
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 3, Clock: clock,
			Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
			UseResilienceBudget: true, HistoryLimit: historyLimit,
		})
		operationErr := retry.Retryable(errors.New("downstream"))
		calls := 0
		result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, operationErr
		})
		var budgetErr *retry.BudgetError
		var rejection *resilience.BudgetRejectionError
		if calls != 2 || !errors.As(err, &budgetErr) || budgetErr.Kind != retry.BudgetWork || err.Error() != "retry work budget exhausted: retry work admission failed" || len(err.Error()) > retry.MaxStrictTerminalErrorBytes || !errors.Is(err, resilience.ErrBudgetRejected) || !errors.As(err, &rejection) || result.Value != "" || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonWorkBudget || !equalRetryResult(budgetErr.Result(), result.Retry) {
			t.Fatalf("history=%d calls=%d result=%+v err=%v", historyLimit, calls, result, err)
		}
		assertStrictCarrier(t, err, []error{rejection})
		assertDefensiveStrictResult(t, budgetErr.Result, result.Retry)
		if len(result.Retry.History) != int(historyLimit) {
			t.Fatalf("history=%+v", result.Retry.History)
		}
		for index, entry := range result.Retry.History {
			//nolint:errorlint // History must retain the exact operation error.
			if entry.Attempt != uint(index+1) || entry.Classification != retry.ClassificationRetryable || entry.Err != operationErr || entry.Delay != 0 {
				t.Fatalf("history[%d]=%+v", index, entry)
			}
		}
		if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 1 || snapshot.AdditionalActive != 0 {
			t.Fatalf("snapshot=%+v", snapshot)
		}
	}
}

func TestDoStrictCancellationDuringWorkAdmissionUsesContextOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cancelAt    int
		wantCalls   int
		wantOutcome retry.Outcome
	}{
		{name: "initial admission", cancelAt: 2, wantCalls: 0, wantOutcome: retry.OutcomeNotDispatched},
		{name: "retry admission", cancelAt: 6, wantCalls: 1, wantOutcome: retry.OutcomeKnown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newManualClock(time.Unix(600, 0))
			budgetValue, err := resilience.NewBudget(resilience.BudgetConfig{
				MaxResources: 1, MaxAdditionalPerExecution: 2,
				MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 2,
				AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock,
			})
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
			if err != nil {
				t.Fatal(err)
			}
			scope, attached, err := budgetValue.Start(context.Background(), metadata)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = scope.Close() }()
			ctx := &cancelDuringAdmissionContext{Context: attached, cancelAt: test.cancelAt}
			policy := mustStrictPolicy(t, retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 2, Clock: clock,
				Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(),
				UseResilienceBudget: true,
			})
			calls := 0
			result, executionErr := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
				calls++
				return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
			})
			var contextErr *retry.ContextError
			var budgetErr *retry.BudgetError
			if calls != test.wantCalls || !errors.As(executionErr, &contextErr) || errors.As(executionErr, &budgetErr) || !errors.Is(executionErr, context.Canceled) || result.Outcome != test.wantOutcome || result.Retry.Reason != retry.ReasonCanceled {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, executionErr)
			}
		})
	}
}

func TestDoStrictCompletesPermitWhenCancellationLandsAfterAdmission(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		cancelOnAcquire int
		wantCalls       int
		wantOutcome     retry.Outcome
	}{
		{name: "initial admission", cancelOnAcquire: 1, wantCalls: 0, wantOutcome: retry.OutcomeNotDispatched},
		{name: "retry admission", cancelOnAcquire: 2, wantCalls: 1, wantOutcome: retry.OutcomeKnown},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			scope := &postAdmissionCancelScope{cancelOnAcquire: test.cancelOnAcquire, cancel: cancel}
			ctx, err := resilience.WithBudgetScope(base, scope)
			if err != nil {
				t.Fatal(err)
			}
			clock := newManualClock(time.Unix(650, 0))
			policy := mustStrictPolicy(t, retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 2, Clock: clock,
				Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(),
				UseResilienceBudget: true,
			})
			calls := 0
			result, executionErr := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
				calls++
				return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
			})
			var contextErr *retry.ContextError
			if calls != test.wantCalls || !errors.As(executionErr, &contextErr) || !errors.Is(executionErr, context.Canceled) || result.Outcome != test.wantOutcome || result.Retry.Reason != retry.ReasonCanceled {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, executionErr)
			}
			if len(scope.permits) != test.cancelOnAcquire {
				t.Fatalf("permits=%d", len(scope.permits))
			}
			for index, permit := range scope.permits {
				if permit.completions != 1 {
					t.Fatalf("permit %d completions=%d", index, permit.completions)
				}
			}
		})
	}
}

func TestDoStrictCompletesUndispatchedPermitOnElapsedExitAndClockPanic(t *testing.T) {
	t.Run("elapsed exit", func(t *testing.T) {
		start := time.Unix(660, 0)
		clock := &sequenceClock{times: []time.Time{start, start, start.Add(2 * time.Second), start.Add(2 * time.Second)}}
		scope := &postAdmissionCancelScope{cancelOnAcquire: 99}
		ctx, err := resilience.WithBudgetScope(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 1, MaxElapsed: time.Second,
			Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true,
		})
		result, executionErr := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			t.Fatal("operation dispatched after elapsed budget")
			return retry.AttemptResult[string]{}, nil
		})
		var budget *retry.BudgetError
		if !errors.As(executionErr, &budget) || budget.Kind != retry.BudgetElapsed || result.Outcome != retry.OutcomeNotDispatched || len(scope.permits) != 1 || scope.permits[0].completions != 1 {
			t.Fatalf("result=%+v err=%v permits=%+v", result, executionErr, scope.permits)
		}
	})

	t.Run("clock panic", func(t *testing.T) {
		want := errors.New("clock panic")
		clock := &panicAfterAdmissionClock{now: time.Unix(670, 0), panicValue: want}
		scope := &postAdmissionCancelScope{cancelOnAcquire: 99}
		ctx, err := resilience.WithBudgetScope(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 1, MaxElapsed: time.Second,
			Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: true,
		})
		defer func() {
			//nolint:errorlint // Panic value identity is part of the contract.
			if recovered := recover(); recovered != want || len(scope.permits) != 1 || scope.permits[0].completions != 1 {
				t.Fatalf("recovered=%v permits=%+v", recovered, scope.permits)
			}
		}()
		_, _ = retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			t.Fatal("operation dispatched after clock panic")
			return retry.AttemptResult[string]{}, nil
		})
	})
}

func TestDoStrictCancelsAttemptContextWhenOperationOrPermitPanics(t *testing.T) {
	want := errors.New("panic")
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
			policy := mustStrictPolicy(t, retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 1, AttemptTimeout: time.Second,
				Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(), UseResilienceBudget: role == "permit",
			})
			defer func() {
				permitCalls := 0
				if permit != nil {
					permitCalls = permit.calls
				}
				//nolint:errorlint // Panic value identity is part of the contract.
				if recovered := recover(); recovered != want || clock.timeouts != 1 || clock.cancels != 1 || permitCalls != map[string]int{"operation": 0, "permit": 1}[role] {
					t.Fatalf("recovered=%v timeouts=%d cancels=%d permit=%d", recovered, clock.timeouts, clock.cancels, permitCalls)
				}
			}()
			_, _ = retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
				if role == "operation" {
					panic(want)
				}
				return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
			})
		})
	}
}

func TestDoStrictObserverPanicIsIsolatedAndOperationPanicPropagates(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	config := baseConfig(clock, retry.Config{})
	config.Observer = retry.ObserveFunc(func(retry.Observation) { panic("observer panic") })
	policy := mustStrictPolicy(t, config)
	result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || result.Retry.Reason != retry.ReasonSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}

	calls := 0
	defer func() {
		if recovered := recover(); recovered != "operation panic" || calls != 1 {
			t.Fatalf("recovered=%v calls=%d", recovered, calls)
		}
	}()
	_, _ = retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		calls++
		panic("operation panic")
	})
}

func TestObserveFuncDirectPanicPropagates(t *testing.T) {
	t.Parallel()

	want := errors.New("observer panic")
	observer := retry.ObserveFunc(func(retry.Observation) { panic(want) })
	defer func() {
		//nolint:errorlint // Panic value identity is part of the contract.
		if recovered := recover(); recovered != want {
			t.Fatalf("recovered = %v", recovered)
		}
	}()
	observer.Observe(retry.Observation{})
}

func TestDoStrictCallbackOrderingIdentityAndCardinality(t *testing.T) {
	t.Parallel()

	parent := context.WithValue(context.Background(), strictContextKey{}, "identity")
	clock := newManualClock(time.Unix(700, 0))
	classifications := 0
	observations := make([]retry.Observation, 0, 2)
	config := retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2, Clock: clock,
		Sleeper: noOpSleeper{},
		Classifier: retry.ClassifyFunc(func(ctx context.Context, err error) (retry.Classification, error) {
			classifications++
			if ctx != parent || !errors.Is(err, errStrictTemporary) {
				t.Fatalf("classifier ctx=%v err=%v", ctx, err)
			}
			return retry.ClassificationRetryable, nil
		}),
		Observer: retry.ObserveFunc(func(observation retry.Observation) {
			observations = append(observations, observation)
		}),
	}
	policy := mustStrictPolicy(t, config)
	calls := 0
	result, err := retry.DoStrict(parent, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
		calls++
		if ctx != parent || ctx.Value(strictContextKey{}) != "identity" {
			t.Fatalf("operation context identity changed: %v", ctx)
		}
		if calls == 1 {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, errStrictTemporary
		}
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || calls != 2 || classifications != 1 || len(observations) != 2 || observations[0].Reason != "" || observations[1].Reason != retry.ReasonSucceeded {
		t.Fatalf("calls=%d classifications=%d observations=%+v result=%+v err=%v", calls, classifications, observations, result, err)
	}

	classifications = 0
	for _, operation := range []retry.StrictOperation[string]{
		func(context.Context) (retry.AttemptResult[string], error) {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeUnknown}, errors.New("ambiguous")
		},
		func(context.Context) (retry.AttemptResult[string], error) {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeNotDispatched}, errors.New("invalid")
		},
	} {
		_, _ = retry.DoStrict(parent, policy, operation)
	}
	if classifications != 0 {
		t.Fatalf("unknown or invalid outcomes classified %d times", classifications)
	}
}

func TestDoStrictInvokesEveryPolicyCollaboratorInReleasedOrder(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), strictContextKey{}, "collaborator identity")
	trace := &strictCallbackTrace{now: time.Unix(1000, 0)}
	random := &tracedStrictRandom{trace: trace}
	hint := &tracedStrictHint{trace: trace}
	config := retry.Config{
		Backoff: &tracedStrictBackoff{trace: trace, random: random}, MaxAttempts: 2,
		Clock: trace, Sleeper: tracedStrictSleeper{trace: trace, wantContext: ctx}, Random: random,
		Classifier: tracedStrictClassifier{trace: trace, wantContext: ctx, wantError: hint},
		Observer: retry.ObserveFunc(func(observation retry.Observation) {
			trace.add("observer")
			if observation.Attempt == 0 {
				t.Fatal("observer lost attempt identity")
			}
		}),
	}
	policy := mustStrictPolicy(t, config)
	calls := 0
	result, err := retry.DoStrict(ctx, policy, func(got context.Context) (retry.AttemptResult[string], error) {
		calls++
		trace.add(fmt.Sprintf("operation%d", calls))
		if got != ctx {
			t.Fatal("operation context identity changed")
		}
		if calls == 1 {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, hint
		}
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	want := []string{"clock", "operation1", "classifier", "clock", "backoff", "random", "clock", "hint", "clock", "observer", "sleeper", "operation2", "clock", "observer"}
	if err != nil || result.Value != "done" || calls != 2 || hint.calls != 1 || !reflect.DeepEqual(trace.events, want) {
		t.Fatalf("events=%v calls=%d hint=%d result=%+v err=%v", trace.events, calls, hint.calls, result, err)
	}
}

func TestDoStrictUsesTimeoutClockDerivedAttemptContextExactlyOnce(t *testing.T) {
	t.Parallel()

	parent := context.WithValue(context.Background(), strictContextKey{}, "parent")
	derived := context.WithValue(parent, strictContextKey{}, "attempt")
	clock := &tracedTimeoutClock{now: time.Unix(1100, 0), wantParent: parent, derived: derived}
	policy := mustStrictPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, AttemptTimeout: time.Second,
		Clock: clock, Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(),
	})
	result, err := retry.DoStrict(parent, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
		if ctx != derived || ctx.Value(strictContextKey{}) != "attempt" {
			t.Fatalf("attempt context = %v", ctx)
		}
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || clock.timeouts != 1 || clock.cancels != 1 || clock.duration != time.Second {
		t.Fatalf("result=%+v err=%v timeouts=%d cancels=%d duration=%s", result, err, clock.timeouts, clock.cancels, clock.duration)
	}
}

func TestClassifyFuncDirectPanicPropagates(t *testing.T) {
	t.Parallel()

	want := errors.New("classifier panic")
	classifier := retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { panic(want) })
	defer func() {
		//nolint:errorlint // Panic value identity is part of the direct callback contract.
		if recovered := recover(); recovered != want {
			t.Fatalf("recovered = %v", recovered)
		}
	}()
	_, _ = classifier.Classify(context.Background(), errors.New("operation"))
}

func TestDoStrictDeliversReachedTerminalObservationAfterCancellationRace(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	clock := newManualClock(time.Unix(800, 0))
	var observations []retry.Observation
	config := baseConfig(clock, retry.Config{})
	config.Observer = retry.ObserveFunc(func(observation retry.Observation) { observations = append(observations, observation) })
	policy := mustStrictPolicy(t, config)
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		cancel()
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || len(observations) != 1 || observations[0].Attempt != 1 || observations[0].Reason != retry.ReasonSucceeded {
		t.Fatalf("observations=%+v result=%+v err=%v", observations, result, err)
	}
}

func TestDoStrictOperationIsSynchronousConcurrentAndReentrant(t *testing.T) {
	t.Run("blocking and concurrent", func(t *testing.T) {
		clock := newManualClock(time.Unix(900, 0))
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
			Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(),
		})
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		operation := retry.StrictOperation[string](func(context.Context) (retry.AttemptResult[string], error) {
			entered <- struct{}{}
			<-release
			return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
		})
		done := make(chan struct{}, 2)
		for range 2 {
			go func() { _, _ = retry.DoStrict(context.Background(), policy, operation); done <- struct{}{} }()
		}
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("concurrent operation did not enter")
			}
		}
		select {
		case <-done:
			close(release)
			t.Fatal("DoStrict returned while operation blocked")
		default:
		}
		close(release)
		for range 2 {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("DoStrict did not return")
			}
		}
	})

	t.Run("reentrant", func(t *testing.T) {
		clock := newManualClock(time.Unix(901, 0))
		policy := mustStrictPolicy(t, retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
			Sleeper: noOpSleeper{}, Classifier: retry.RetryableClassifier(),
		})
		calls := 0
		outer, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			inner, innerErr := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
				calls++
				return retry.AttemptResult[string]{Value: "inner", Outcome: retry.OutcomeKnown}, nil
			})
			if innerErr != nil || inner.Value != "inner" {
				t.Fatalf("inner=%+v err=%v", inner, innerErr)
			}
			return retry.AttemptResult[string]{Value: "outer", Outcome: retry.OutcomeKnown}, nil
		})
		if err != nil || outer.Value != "outer" || calls != 2 {
			t.Fatalf("outer=%+v calls=%d err=%v", outer, calls, err)
		}
	})
}

func strictRetryableFailure(context.Context) (retry.AttemptResult[string], error) {
	return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
}

type fixedAdvancingSleeper struct {
	clock   *manualClock
	advance time.Duration
}

type strictContextKey struct{}

var errStrictTemporary = errors.New("strict temporary")

func (sleeper fixedAdvancingSleeper) Sleep(context.Context, time.Duration) error {
	sleeper.clock.advance(sleeper.advance)
	return nil
}

type cancelAndFailSleeper struct {
	cancel context.CancelFunc
	err    error
}

type mutableStrictContext struct{ err error }

func (*mutableStrictContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*mutableStrictContext) Done() <-chan struct{}       { return nil }
func (ctx *mutableStrictContext) Err() error              { return ctx.err }
func (*mutableStrictContext) Value(any) any               { return nil }

type deadlineSettingSleeper struct{ ctx *mutableStrictContext }

func (sleeper deadlineSettingSleeper) Sleep(context.Context, time.Duration) error {
	sleeper.ctx.err = context.DeadlineExceeded
	return nil
}

func (sleeper cancelAndFailSleeper) Sleep(context.Context, time.Duration) error {
	sleeper.cancel()
	return sleeper.err
}

type sequencedErrorContext struct {
	calls    int
	cancelAt int
}

func (*sequencedErrorContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*sequencedErrorContext) Done() <-chan struct{}       { return nil }
func (ctx *sequencedErrorContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
func (*sequencedErrorContext) Value(any) any { return nil }

type cancelDuringAdmissionContext struct {
	context.Context
	calls    int
	cancelAt int
}

type postAdmissionCancelScope struct {
	acquires        int
	cancelOnAcquire int
	cancel          context.CancelFunc
	permits         []*postAdmissionPermit
}

func (scope *postAdmissionCancelScope) Acquire(context.Context, resilience.Attempt) (resilience.Permit, error) {
	scope.acquires++
	permit := &postAdmissionPermit{}
	scope.permits = append(scope.permits, permit)
	if scope.acquires == scope.cancelOnAcquire {
		scope.cancel()
	}
	return permit, nil
}

func (*postAdmissionCancelScope) Snapshot() resilience.BudgetSnapshot {
	return resilience.BudgetSnapshot{}
}
func (*postAdmissionCancelScope) Matches(resilience.Metadata) bool { return true }
func (*postAdmissionCancelScope) Close() error                     { return nil }

type postAdmissionPermit struct{ completions int }

func (permit *postAdmissionPermit) Complete() error {
	permit.completions++
	return nil
}

type panicAfterAdmissionClock struct {
	now        time.Time
	calls      int
	panicValue any
}

func (clock *panicAfterAdmissionClock) Now() time.Time {
	clock.calls++
	if clock.calls == 3 {
		panic(clock.panicValue)
	}
	return clock.now
}

type cancelTrackingTimeoutClock struct {
	now      time.Time
	timeouts int
	cancels  int
}

func (clock *cancelTrackingTimeoutClock) Now() time.Time { return clock.now }
func (clock *cancelTrackingTimeoutClock) WithTimeout(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
	clock.timeouts++
	derived, cancel := context.WithCancel(ctx)
	return derived, func() {
		clock.cancels++
		cancel()
	}
}

type fixedPermitScope struct{ permit resilience.Permit }

func (scope fixedPermitScope) Acquire(context.Context, resilience.Attempt) (resilience.Permit, error) {
	return scope.permit, nil
}
func (fixedPermitScope) Snapshot() resilience.BudgetSnapshot { return resilience.BudgetSnapshot{} }
func (fixedPermitScope) Matches(resilience.Metadata) bool    { return true }
func (fixedPermitScope) Close() error                        { return nil }

type panickingCompletionPermit struct {
	calls      int
	panicValue any
}

func (permit *panickingCompletionPermit) Complete() error {
	permit.calls++
	panic(permit.panicValue)
}

type strictCallbackTrace struct {
	now    time.Time
	events []string
}

type tracedTimeoutClock struct {
	now        time.Time
	wantParent context.Context
	derived    context.Context
	duration   time.Duration
	timeouts   int
	cancels    int
}

func (clock *tracedTimeoutClock) Now() time.Time { return clock.now }
func (clock *tracedTimeoutClock) WithTimeout(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	if parent != clock.wantParent {
		panic("timeout parent changed")
	}
	clock.timeouts++
	clock.duration = duration
	return clock.derived, func() { clock.cancels++ }
}

func (trace *strictCallbackTrace) add(event string) { trace.events = append(trace.events, event) }
func (trace *strictCallbackTrace) Now() time.Time {
	trace.add("clock")
	return trace.now
}

type tracedStrictRandom struct{ trace *strictCallbackTrace }

func (random *tracedStrictRandom) Int64n(upper int64) int64 {
	random.trace.add("random")
	if upper != 7 {
		panic("random upper changed")
	}
	return 0
}

type tracedStrictBackoff struct {
	trace  *strictCallbackTrace
	random retry.Random
}

func (backoff *tracedStrictBackoff) Delay(attempt uint, previous time.Duration, random retry.Random) time.Duration {
	backoff.trace.add("backoff")
	if attempt != 1 || previous != 0 || random != backoff.random {
		panic("backoff arguments changed")
	}
	_ = random.Int64n(7)
	return time.Second
}

type tracedStrictClassifier struct {
	trace       *strictCallbackTrace
	wantContext context.Context
	wantError   error
}

func (classifier tracedStrictClassifier) Classify(ctx context.Context, err error) (retry.Classification, error) {
	classifier.trace.add("classifier")
	//nolint:errorlint // The classifier must receive the exact borrowed operation error.
	if ctx != classifier.wantContext || err != classifier.wantError {
		panic("classifier arguments changed")
	}
	return retry.ClassificationRetryable, nil
}

type tracedStrictSleeper struct {
	trace       *strictCallbackTrace
	wantContext context.Context
}

func (sleeper tracedStrictSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	sleeper.trace.add("sleeper")
	if ctx != sleeper.wantContext || delay != time.Second {
		panic("sleeper arguments changed")
	}
	return nil
}

type tracedStrictHint struct {
	trace *strictCallbackTrace
	calls int
}

func (*tracedStrictHint) Error() string { return "hinted" }
func (hint *tracedStrictHint) RetryDelay(now time.Time) (time.Duration, bool) {
	hint.trace.add("hint")
	hint.calls++
	if now != hint.trace.now {
		panic("delay-hint time changed")
	}
	return 0, false
}

func (ctx *cancelDuringAdmissionContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}
