package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v3"
)

func TestNewPolicyRejectsTypedNilCollaborators(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	valid := retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
	}
	var backoff *nilBackoff
	var requiredClock *nilClock
	var sleeper *nilSleeper
	var classifier *nilClassifier
	var random *nilRandom
	var observer *nilObserver

	tests := []struct {
		name   string
		config retry.Config
		want   string
	}{
		{"backoff", withStrictBackoff(valid, backoff), "invalid retry policy: backoff is required"},
		{"clock", withStrictClock(valid, requiredClock), "invalid retry policy: clock is required"},
		{"sleeper", withStrictSleeper(valid, sleeper), "invalid retry policy: sleeper is required"},
		{"classifier", withStrictClassifier(valid, classifier), "invalid retry policy: classifier is required"},
		{"random", withStrictRandom(valid, random), "invalid retry policy: random is typed nil"},
		{"observer", withStrictObserver(valid, observer), "invalid retry policy: observer is typed nil"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy, err := retry.NewPolicy(test.config)
			if policy != nil || err == nil || err.Error() != test.want || !errors.Is(err, retry.ErrInvalidPolicy) {
				t.Fatalf("NewPolicy = (%v, %v), want (nil, %q)", policy, err, test.want)
			}
		})
	}
}

func TestNewPolicyRejectsLiteralNilRequiredCollaborators(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	valid := retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
	}
	tests := []struct {
		config retry.Config
		want   string
	}{
		{withStrictBackoff(valid, nil), "invalid retry policy: backoff is required"},
		{withStrictClock(valid, nil), "invalid retry policy: clock is required"},
		{withStrictSleeper(valid, nil), "invalid retry policy: sleeper is required"},
		{withStrictClassifier(valid, nil), "invalid retry policy: classifier is required"},
	}
	for _, test := range tests {
		if policy, err := retry.NewPolicy(test.config); policy != nil || err == nil || err.Error() != test.want || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("NewPolicy = (%v,%v), want %q", policy, err, test.want)
		}
	}
}

func TestNewPolicyAcceptsAbsentOptionalCollaborators(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	config := retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
	}
	if _, err := retry.NewPolicy(config); err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
}

func TestNewPolicyAcceptsUsableOptionalCollaborators(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Unix(100, 0))
	random := &countingStrictRandom{}
	observed := 0
	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.FullJitter(retry.Constant(time.Second)), MaxAttempts: 2,
		Clock: clock, Sleeper: advancingSleeper{clock}, Random: random,
		Classifier: retry.RetryableClassifier(),
		Observer:   retry.ObserveFunc(func(retry.Observation) { observed++ }),
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := retry.Do(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		calls++
		if calls == 1 {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
		}
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || random.calls != 1 || observed != 2 {
		t.Fatalf("result=%+v err=%v random=%d observed=%d", result, err, random.calls, observed)
	}
}

type nilRandom struct{}

func (*nilRandom) Int64n(int64) int64 { panic("typed-nil random invoked") }

type nilObserver struct{}

func (*nilObserver) Observe(retry.Observation) { panic("typed-nil observer invoked") }

type countingStrictRandom struct{ calls int }

func (random *countingStrictRandom) Int64n(int64) int64 {
	random.calls++
	return 0
}

func withStrictBackoff(config retry.Config, value retry.Backoff) retry.Config {
	config.Backoff = value
	return config
}

func withStrictClock(config retry.Config, value retry.Clock) retry.Config {
	config.Clock = value
	return config
}

func withStrictSleeper(config retry.Config, value retry.Sleeper) retry.Config {
	config.Sleeper = value
	return config
}

func withStrictClassifier(config retry.Config, value retry.Classifier) retry.Config {
	config.Classifier = value
	return config
}

func withStrictRandom(config retry.Config, value retry.Random) retry.Config {
	config.Random = value
	return config
}

func withStrictObserver(config retry.Config, value retry.Observer) retry.Config {
	config.Observer = value
	return config
}

var _ context.Context = (*nilContext)(nil)

type nilContext struct{}

func (*nilContext) Deadline() (time.Time, bool) { panic("typed-nil context invoked") }
func (*nilContext) Done() <-chan struct{}       { panic("typed-nil context invoked") }
func (*nilContext) Err() error                  { panic("typed-nil context invoked") }
func (*nilContext) Value(any) any               { panic("typed-nil context invoked") }
