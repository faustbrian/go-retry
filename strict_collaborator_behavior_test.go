package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
)

func TestDoStrictCollaboratorsAreBlockingConcurrentAndReentrant(t *testing.T) {
	roles := []string{"backoff", "clock", "timeout clock", "random", "sleeper", "classifier", "delay hint", "observer"}
	for _, role := range roles {
		t.Run(role+" blocking concurrent", func(t *testing.T) {
			entered := make(chan struct{}, 64)
			release := make(chan struct{})
			run := newStrictCollaboratorRunner(t, role, func() {
				entered <- struct{}{}
				<-release
			})
			done := make(chan struct{}, 2)
			for range 2 {
				go func() {
					_ = run(context.Background())
					done <- struct{}{}
				}()
			}
			for range 2 {
				select {
				case <-entered:
				case <-time.After(time.Second):
					close(release)
					t.Fatal("separate executions did not enter collaborator concurrently")
				}
			}
			select {
			case <-done:
				close(release)
				t.Fatal("execution returned while collaborator was blocked")
			default:
			}
			close(release)
			for range 2 {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("execution did not return after collaborator release")
				}
			}
		})

		t.Run(role+" reentrant", func(t *testing.T) {
			var run func(context.Context) error
			reentered := false
			hook := func() {
				if reentered {
					return
				}
				reentered = true
				if err := run(context.Background()); err != nil {
					t.Fatalf("reentrant execution: %v", err)
				}
			}
			run = newStrictCollaboratorRunner(t, role, hook)
			if err := run(context.Background()); err != nil || !reentered {
				t.Fatalf("execution=%v reentered=%v", err, reentered)
			}
		})
	}
}

func TestDoStrictCollaboratorPanicRules(t *testing.T) {
	want := errors.New("collaborator panic")
	for _, role := range []string{"backoff", "clock", "timeout clock", "random", "sleeper", "delay hint"} {
		t.Run(role, func(t *testing.T) {
			run := newStrictCollaboratorRunner(t, role, func() { panic(want) })
			defer func() {
				//nolint:errorlint // Panic value identity is part of the collaborator contract.
				if recovered := recover(); recovered != want {
					t.Fatalf("recovered = %v", recovered)
				}
			}()
			_ = run(context.Background())
		})
	}
	t.Run("classifier sanitized", func(t *testing.T) {
		run := newStrictCollaboratorRunner(t, "classifier", func() { panic(want) })
		err := run(context.Background())
		if err == nil || err.Error() != "invalid retry policy: classifier panicked" || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("observer isolated", func(t *testing.T) {
		run := newStrictCollaboratorRunner(t, "observer", func() { panic(want) })
		if err := run(context.Background()); err != nil {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestDoStrictDoesNotPreemptEnteredCollaborators(t *testing.T) {
	for _, role := range []string{"backoff", "clock", "timeout clock", "random", "sleeper", "classifier", "delay hint", "observer"} {
		t.Run(role, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			run := newStrictCollaboratorRunner(t, role, func() {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
			})
			done := make(chan struct{})
			go func() {
				_ = run(ctx)
				close(done)
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("collaborator was not entered")
			}
			cancel()
			select {
			case <-done:
				close(release)
				t.Fatal("cancellation preempted an entered collaborator")
			default:
			}
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("execution did not return after collaborator release")
			}
		})
	}
}

func newStrictCollaboratorRunner(t *testing.T, role string, hook func()) func(context.Context) error {
	t.Helper()
	clock := retry.Clock(strictBehaviorClock{})
	backoff := retry.Backoff(retry.Constant(0))
	random := retry.Random(nil)
	sleeper := retry.Sleeper(strictBehaviorSleeper{})
	classifier := retry.Classifier(retry.RetryableClassifier())
	observer := retry.Observer(nil)
	attemptTimeout := time.Duration(0)

	switch role {
	case "clock":
		clock = strictBehaviorClock{hook: hook}
	case "timeout clock":
		clock = strictBehaviorTimeoutClock{hook: hook}
		attemptTimeout = time.Second
	case "backoff":
		backoff = strictBehaviorBackoff{hook: hook}
	case "random":
		random = strictBehaviorRandom{hook: hook}
		backoff = strictBehaviorBackoff{random: true}
	case "sleeper":
		sleeper = strictBehaviorSleeper{hook: hook}
	case "classifier":
		classifier = strictBehaviorClassifier{hook: hook}
	case "observer":
		observer = retry.ObserveFunc(func(retry.Observation) { hook() })
	case "delay hint":
	default:
		t.Fatalf("unknown collaborator role %q", role)
	}

	policy := mustStrictPolicy(t, retry.Config{
		Backoff: backoff, MaxAttempts: 2, AttemptTimeout: attemptTimeout,
		Clock: clock, Sleeper: sleeper, Random: random, Classifier: classifier, Observer: observer,
	})
	return func(ctx context.Context) error {
		calls := 0
		_, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
			calls++
			if calls == 1 {
				if role == "delay hint" {
					return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(strictBehaviorHint{hook: hook})
				}
				return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
			}
			return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
		})
		return err
	}
}

type strictBehaviorClock struct{ hook func() }

func (clock strictBehaviorClock) Now() time.Time {
	if clock.hook != nil {
		clock.hook()
	}
	return time.Unix(1200, 0)
}

type strictBehaviorTimeoutClock struct{ hook func() }

func (strictBehaviorTimeoutClock) Now() time.Time { return time.Unix(1200, 0) }
func (clock strictBehaviorTimeoutClock) WithTimeout(ctx context.Context, _ time.Duration) (context.Context, context.CancelFunc) {
	clock.hook()
	return context.WithCancel(ctx)
}

type strictBehaviorBackoff struct {
	hook   func()
	random bool
}

func (backoff strictBehaviorBackoff) Delay(_ uint, _ time.Duration, random retry.Random) time.Duration {
	if backoff.hook != nil {
		backoff.hook()
	}
	if backoff.random {
		_ = random.Int64n(1)
	}
	return 0
}

type strictBehaviorRandom struct{ hook func() }

func (random strictBehaviorRandom) Int64n(int64) int64 {
	random.hook()
	return 0
}

type strictBehaviorSleeper struct{ hook func() }

func (sleeper strictBehaviorSleeper) Sleep(context.Context, time.Duration) error {
	if sleeper.hook != nil {
		sleeper.hook()
	}
	return nil
}

type strictBehaviorClassifier struct{ hook func() }

func (classifier strictBehaviorClassifier) Classify(context.Context, error) (retry.Classification, error) {
	classifier.hook()
	return retry.ClassificationRetryable, nil
}

type strictBehaviorHint struct{ hook func() }

func (strictBehaviorHint) Error() string { return "temporary" }
func (hint strictBehaviorHint) RetryDelay(time.Time) (time.Duration, bool) {
	hint.hook()
	return 0, false
}
