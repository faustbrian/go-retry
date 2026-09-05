package retry_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/go-resilience"
	retry "github.com/faustbrian/go-retry"
)

func TestContextErrorZeroAndNilReceiversAreTotal(t *testing.T) {
	t.Parallel()

	for _, err := range []*retry.ContextError{{}, nil} {
		if err.Error() != "retry context error" || err.Unwrap() != nil || err.Outcome() != retry.OutcomeNotDispatched || err.Is(retry.ErrOutcomeUnknown) {
			t.Fatalf("zero ContextError = (%q,%v,%v)", err.Error(), err.Unwrap(), err.Outcome())
		}
		result := err.Result()
		if result.Attempts != 0 || result.Reason != "" || len(result.History) != 0 {
			t.Fatalf("zero Result = %+v", result)
		}
	}
}

func TestContextErrorResultIsDefensive(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	clock := newManualClock(time.Unix(100, 0))
	policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		cancel()
		return retry.AttemptResult[string]{Outcome: retry.OutcomeUnknown}, errors.New("ambiguous")
	})
	var contextErr *retry.ContextError
	if !errors.As(err, &contextErr) || len(result.Retry.History) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result.Retry.History[0].Attempt = 99
	first := contextErr.Result()
	first.History[0].Attempt = 88
	second := contextErr.Result()
	if second.History[0].Attempt != 1 {
		t.Fatalf("stored result aliased: %+v", second)
	}
}

func TestDoStrictKnownFailuresRetryAndWinCancellationRace(t *testing.T) {
	t.Parallel()

	t.Run("known retry then success", func(t *testing.T) {
		clock := newManualClock(time.Unix(100, 0))
		policy := mustStrictPolicy(t, baseConfig(clock, retry.Config{}))
		calls := 0
		result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			if calls == 1 {
				return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
			}
			return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
		})
		if err != nil || calls != 2 || result.Value != "done" || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonSucceeded {
			t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
		}
	})

	t.Run("known terminal failure wins cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		clock := newManualClock(time.Unix(100, 0))
		cause := errors.New("known failure")
		config := baseConfig(clock, retry.Config{})
		config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) {
			return retry.ClassificationPermanent, nil
		})
		policy := mustStrictPolicy(t, config)
		result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			cancel()
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, cause
		})
		var permanent *retry.PermanentError
		if !errors.As(err, &permanent) || !errors.Is(err, cause) || errors.Is(err, context.Canceled) || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != retry.ReasonPermanent {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
}

func TestDoStrictKnownTerminalWrappersAreBoundedAndPreserveCauses(t *testing.T) {
	t.Parallel()

	cause := &hostileStrictError{marker: strings.Repeat("secret", 200000)}
	classifierCause := &hostileClassifierError{marker: strings.Repeat("classifier-secret", 100000)}
	sleeperCause := &hostileSleeperError{marker: strings.Repeat("sleeper-secret", 100000)}
	tests := []struct {
		name        string
		config      func(*manualClock) retry.Config
		wantText    string
		wantReason  retry.Reason
		wantKind    retry.BudgetKind
		wantType    string
		causes      []error
		wantHistory int
	}{
		{"permanent", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{})
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return retry.ClassificationPermanent, nil })
			return config
		}, "permanent: retry operation failed", retry.ReasonPermanent, "", "permanent", []error{cause}, 1},
		{"exhausted", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{})
			config.MaxAttempts = 1
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return retry.ClassificationRetryable, nil })
			return config
		}, "retry attempts exhausted: retry operation failed", retry.ReasonAttemptsExhausted, "", "exhausted", []error{cause}, 1},
		{"classifier error", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{})
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return 0, classifierCause })
			return config
		}, "permanent: retry classifier failed", retry.ReasonClassifierFailure, "", "permanent", []error{cause, classifierCause}, 0},
		{"invalid classification", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{})
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return 99, nil })
			return config
		}, "permanent: retry classifier failed", retry.ReasonClassifierFailure, "", "permanent", []error{cause}, 0},
		{"sleeper", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{})
			config.Sleeper = failingSleeper{sleeperCause}
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return retry.ClassificationRetryable, nil })
			return config
		}, "permanent: retry sleeper failed", retry.ReasonSleeperFailure, "", "permanent", []error{sleeperCause}, 1},
		{"elapsed", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{MaxElapsed: time.Second})
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return retry.ClassificationRetryable, nil })
			return config
		}, "retry elapsed budget exhausted: retry operation failed", retry.ReasonElapsedBudget, retry.BudgetElapsed, "budget", []error{cause}, 1},
		{"sleep", func(clock *manualClock) retry.Config {
			config := baseConfig(clock, retry.Config{MaxSleep: time.Second})
			config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return retry.ClassificationRetryable, nil })
			return config
		}, "retry sleep budget exhausted: retry operation failed", retry.ReasonSleepBudget, retry.BudgetSleep, "budget", []error{cause}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, historyLimit := range []uint{0, 2} {
				clock := newManualClock(time.Unix(100, 0))
				config := test.config(clock)
				config.HistoryLimit = historyLimit
				policy := mustStrictPolicy(t, config)
				result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
					return retry.AttemptResult[string]{Value: "discarded", Outcome: retry.OutcomeKnown}, cause
				})
				if err == nil || err.Error() != test.wantText || len(err.Error()) > retry.MaxStrictTerminalErrorBytes || strings.Contains(err.Error(), "secret") || result.Value != "" || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != test.wantReason {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				wantHistory := 0
				if historyLimit > 0 {
					wantHistory = test.wantHistory
				}
				if len(result.Retry.History) != wantHistory {
					t.Fatalf("history=%+v, want %d entries", result.Retry.History, wantHistory)
				}
				if wantHistory > 0 {
					wantClassification := retry.ClassificationRetryable
					wantDelay := 2 * time.Second
					switch test.name {
					case "permanent":
						wantClassification = retry.ClassificationPermanent
						wantDelay = 0
					case "exhausted":
						wantDelay = 0
					}
					entry := result.Retry.History[0]
					//nolint:errorlint // History must retain the exact operation cause.
					if entry.Attempt != 1 || entry.Classification != wantClassification || entry.Err != cause || entry.Delay != wantDelay {
						t.Fatalf("history entry=%+v", entry)
					}
				}
				for _, want := range test.causes {
					if !errors.Is(err, want) {
						t.Fatalf("safe error does not reach %T", want)
					}
				}
				assertStrictCarrier(t, err, test.causes)
				assertStrictTypedCauses(t, err, test.causes)
				switch test.wantType {
				case "permanent":
					var typed *retry.PermanentError
					if !errors.As(err, &typed) {
						t.Fatalf("error type = %T", err)
					}
				case "exhausted":
					var typed *retry.ExhaustedError
					if !errors.As(err, &typed) || !equalRetryResult(typed.Result(), result.Retry) {
						t.Fatalf("error type/result = %T", err)
					}
					assertDefensiveStrictResult(t, typed.Result, result.Retry)
				case "budget":
					var typed *retry.BudgetError
					if !errors.As(err, &typed) || typed.Kind != test.wantKind || !equalRetryResult(typed.Result(), result.Retry) {
						t.Fatalf("error type/kind/result = %T %+v", err, typed)
					}
					assertDefensiveStrictResult(t, typed.Result, result.Retry)
				}
			}
		})
	}
}

func TestDoStrictAttemptTimeoutAndWorkBudgetErrorsAreSafe(t *testing.T) {
	t.Parallel()

	cause := &hostileTimeoutError{marker: strings.Repeat("timeout-secret", 100000)}
	clock := immediateTimeoutClock{newManualClock(time.Unix(100, 0))}
	policy := mustStrictPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2, AttemptTimeout: time.Second,
		Clock: clock, Sleeper: advancingSleeper{clock.manualClock},
		Classifier: retry.RetryableClassifier(),
	})
	result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, cause
	})
	var budget *retry.BudgetError
	if !errors.As(err, &budget) || budget.Kind != retry.BudgetAttempt || err.Error() != "retry attempt budget exhausted: retry attempt timed out" || !errors.Is(err, cause) || !errors.Is(err, context.DeadlineExceeded) || result.Retry.Reason != retry.ReasonAttemptBudget {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Retry.History) != 0 || !equalRetryResult(budget.Result(), result.Retry) {
		t.Fatalf("attempt result=%+v", result.Retry)
	}
	assertStrictCarrier(t, err, []error{cause, context.DeadlineExceeded})
	assertStrictTypedCauses(t, err, []error{cause})

	elapsedClock := immediateTimeoutClock{newManualClock(time.Unix(150, 0))}
	elapsedPolicy := mustStrictPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 2, MaxElapsed: time.Second,
		Clock: elapsedClock, Sleeper: advancingSleeper{elapsedClock.manualClock},
		Classifier: retry.RetryableClassifier(),
	})
	result, err = retry.DoStrict(context.Background(), elapsedPolicy, func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, cause
	})
	if !errors.As(err, &budget) || budget.Kind != retry.BudgetElapsed || err.Error() != "retry elapsed budget exhausted: retry attempt timed out" || result.Retry.Reason != retry.ReasonElapsedBudget {
		t.Fatalf("elapsed attempt result=%+v err=%v", result, err)
	}
	if !equalRetryResult(budget.Result(), result.Retry) {
		t.Fatalf("elapsed attempt budget result=%+v", budget.Result())
	}
	assertDefensiveStrictResult(t, budget.Result, result.Retry)
	assertStrictCarrier(t, err, []error{cause, context.DeadlineExceeded})
	assertStrictTypedCauses(t, err, []error{cause})

	plainClock := newManualClock(time.Unix(200, 0))
	workPolicy := mustStrictPolicy(t, retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: plainClock,
		Sleeper: advancingSleeper{plainClock}, Classifier: retry.RetryableClassifier(),
		UseResilienceBudget: true,
	})
	result, err = retry.DoStrict(context.Background(), workPolicy, func(context.Context) (retry.AttemptResult[string], error) {
		t.Fatal("operation dispatched without work scope")
		return retry.AttemptResult[string]{}, nil
	})
	if !errors.As(err, &budget) || budget.Kind != retry.BudgetWork || err.Error() != "retry work budget exhausted: retry work admission failed" || !errors.Is(err, resilience.ErrBudgetScopeRequired) || result.Outcome != retry.OutcomeNotDispatched || result.Retry.Reason != retry.ReasonWorkBudget {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Retry.History) != 0 || !equalRetryResult(budget.Result(), result.Retry) {
		t.Fatalf("work result=%+v", result.Retry)
	}
	assertStrictCarrier(t, err, []error{resilience.ErrBudgetScopeRequired})
}

func TestDoStrictUsesOneDelayHintAndPreservesBudgetCause(t *testing.T) {
	t.Parallel()

	for _, budgetKind := range []retry.BudgetKind{retry.BudgetElapsed, retry.BudgetSleep} {
		for _, historyLimit := range []uint{0, 1} {
			hinted := &strictHintedError{delay: 3 * time.Second}
			clock := newManualClock(time.Unix(100, 0))
			config := retry.Config{
				Backoff: retry.Constant(time.Second), MaxAttempts: 2,
				Clock: clock, Sleeper: advancingSleeper{clock},
				Classifier:   retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return retry.ClassificationRetryable, nil }),
				HistoryLimit: historyLimit,
			}
			if budgetKind == retry.BudgetElapsed {
				config.MaxElapsed = 2 * time.Second
			} else {
				config.MaxSleep = 2 * time.Second
			}
			policy := mustStrictPolicy(t, config)
			result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
				return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, hinted
			})
			var budget *retry.BudgetError
			wantReason := retry.ReasonElapsedBudget
			wantText := "retry elapsed budget exhausted: retry operation failed"
			if budgetKind == retry.BudgetSleep {
				wantReason = retry.ReasonSleepBudget
				wantText = "retry sleep budget exhausted: retry operation failed"
			}
			if hinted.calls != 1 || !errors.As(err, &budget) || budget.Kind != budgetKind || !errors.Is(err, hinted) || err.Error() != wantText || len(err.Error()) > retry.MaxStrictTerminalErrorBytes || result.Value != "" || result.Outcome != retry.OutcomeKnown || result.Retry.Reason != wantReason || result.Retry.FinalDelay != 3*time.Second || len(result.Retry.History) != int(historyLimit) || !equalRetryResult(budget.Result(), result.Retry) {
				t.Fatalf("kind=%s history=%d calls=%d result=%+v err=%v", budgetKind, historyLimit, hinted.calls, result, err)
			}
			assertStrictCarrier(t, err, []error{hinted})
			assertStrictTypedCauses(t, err, []error{hinted})
			assertDefensiveStrictResult(t, budget.Result, result.Retry)
			if historyLimit > 0 {
				entry := result.Retry.History[0]
				//nolint:errorlint // History must retain the exact delay-hint error.
				if entry.Attempt != 1 || entry.Classification != retry.ClassificationRetryable || entry.Err != hinted || entry.Delay != 3*time.Second {
					t.Fatalf("history=%+v", result.Retry.History)
				}
			}
		}
	}
}

func TestDoStrictClassifierPanicIsSanitized(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	operationErr := errors.New("operation secret")
	config := baseConfig(clock, retry.Config{})
	config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) {
		panic("classifier secret")
	})
	observations := 0
	config.Observer = retry.ObserveFunc(func(retry.Observation) { observations++ })
	policy := mustStrictPolicy(t, config)
	calls := 0
	result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		calls++
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, operationErr
	})
	if calls != 1 || observations != 0 || err == nil || err.Error() != "invalid retry policy: classifier panicked" || !errors.Is(err, retry.ErrInvalidPolicy) || errors.Is(err, operationErr) || strings.Contains(err.Error(), "secret") || result.Outcome != retry.OutcomeKnown || result.Retry.Attempts != 1 || result.Retry.Reason != retry.ReasonClassifierFailure {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestLegacyClassifierPanicRetainsFormattedPanicAndOperationCause(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	operationErr := errors.New("legacy operation")
	config := baseConfig(clock, retry.Config{})
	config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) {
		panic("legacy classifier panic")
	})
	policy := mustPolicy(t, config)
	_, result, err := retry.Do(context.Background(), policy, func(context.Context) (string, error) {
		return "", operationErr
	})
	var permanent *retry.PermanentError
	if !errors.As(err, &permanent) || !errors.Is(err, operationErr) || !strings.Contains(err.Error(), "classifier panic: legacy classifier panic") || result.Attempts != 1 || result.Reason != retry.ReasonClassifierFailure {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type hostileStrictError struct{ marker string }

func (err *hostileStrictError) Error() string  { panic("hostile Error called: " + err.marker[:6]) }
func (err *hostileStrictError) String() string { panic("hostile String called: " + err.marker[:6]) }
func (err *hostileStrictError) Format(fmt.State, rune) {
	panic("hostile Format called: " + err.marker[:6])
}

type hostileClassifierError struct{ marker string }

func (err *hostileClassifierError) Error() string {
	panic("hostile classifier Error called: " + err.marker[:6])
}
func (err *hostileClassifierError) String() string {
	panic("hostile classifier String called: " + err.marker[:6])
}
func (err *hostileClassifierError) Format(fmt.State, rune) {
	panic("hostile classifier Format called: " + err.marker[:6])
}

type hostileSleeperError struct{ marker string }

func (err *hostileSleeperError) Error() string {
	panic("hostile sleeper Error called: " + err.marker[:6])
}

type hostileTimeoutError struct{ marker string }

func (err *hostileTimeoutError) Error() string {
	panic("hostile timeout Error called: " + err.marker[:6])
}
func (err *hostileTimeoutError) String() string {
	panic("hostile timeout String called: " + err.marker[:6])
}
func (err *hostileTimeoutError) Format(fmt.State, rune) {
	panic("hostile timeout Format called: " + err.marker[:6])
}
func (err *hostileSleeperError) String() string {
	panic("hostile sleeper String called: " + err.marker[:6])
}
func (err *hostileSleeperError) Format(fmt.State, rune) {
	panic("hostile sleeper Format called: " + err.marker[:6])
}

type strictHintedError struct {
	delay time.Duration
	calls int
}

func (*strictHintedError) Error() string  { panic("hostile hinted Error called") }
func (*strictHintedError) String() string { panic("hostile hinted String called") }
func (*strictHintedError) Format(fmt.State, rune) {
	panic("hostile hinted Format called")
}
func (err *strictHintedError) RetryDelay(time.Time) (time.Duration, bool) {
	err.calls++
	return err.delay, true
}

func assertStrictCarrier(t *testing.T, err error, causes []error) {
	t.Helper()
	outer := errors.Unwrap(err)
	if outer == nil {
		t.Fatal("terminal wrapper has no safe carrier")
	}
	if len(causes) == 1 {
		carrier, ok := outer.(interface{ Unwrap() error })
		//nolint:errorlint // The safe carrier must unwrap to the exact cause.
		if !ok || carrier.Unwrap() != causes[0] {
			t.Fatalf("single carrier = %T", outer)
		}
		return
	}
	carrier, ok := outer.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("multiple carrier = %T", outer)
	}
	got := carrier.Unwrap()
	if len(got) != len(causes) {
		t.Fatalf("carrier causes = %d, want %d", len(got), len(causes))
	}
	for index := range causes {
		//nolint:errorlint // The safe carrier must preserve every exact cause identity.
		if got[index] != causes[index] {
			t.Fatalf("carrier cause %d identity changed", index)
		}
	}
}

func assertStrictTypedCauses(t *testing.T, err error, causes []error) {
	t.Helper()
	for _, cause := range causes {
		//nolint:errorlint // The matrix maps each exact concrete cause identity.
		switch want := cause.(type) {
		case *hostileStrictError:
			var got *hostileStrictError
			if !errors.As(err, &got) || got != want {
				t.Fatal("operation cause did not retain typed identity")
			}
		case *hostileClassifierError:
			var got *hostileClassifierError
			if !errors.As(err, &got) || got != want {
				t.Fatal("classifier cause did not retain typed identity")
			}
		case *hostileSleeperError:
			var got *hostileSleeperError
			if !errors.As(err, &got) || got != want {
				t.Fatal("sleeper cause did not retain typed identity")
			}
		case *hostileTimeoutError:
			var got *hostileTimeoutError
			if !errors.As(err, &got) || got != want {
				t.Fatal("timeout cause did not retain typed identity")
			}
		case *strictHintedError:
			var got *strictHintedError
			if !errors.As(err, &got) || got != want {
				t.Fatal("delay-hint cause did not retain typed identity")
			}
		}
	}
}

func assertDefensiveStrictResult(t *testing.T, get func() retry.Result, want retry.Result) {
	t.Helper()
	first := get()
	if len(first.History) == 0 {
		return
	}
	first.History[0].Attempt = 99
	if equalRetryResult(first, get()) || !equalRetryResult(get(), want) {
		t.Fatal("terminal result aliases caller mutation")
	}
}
