package retry

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestSaturatingArithmeticBoundaries(t *testing.T) {
	t.Parallel()

	if got := saturatingAdd(maxDuration-1, 1); got != maxDuration {
		t.Fatalf("exact maximum sum = %d", got)
	}
	if got := saturatingAdd(maxDuration, 1); got != maxDuration {
		t.Fatalf("overflowing sum = %d", got)
	}
	if got := saturatingMultiply(maxDuration, 1); got != maxDuration {
		t.Fatalf("exact maximum product = %d", got)
	}
	if got := saturatingMultiply(maxDuration, 2); got != maxDuration {
		t.Fatalf("overflowing product = %d", got)
	}
	if got := saturatingMultiply(0, math.MaxUint64); got != 0 {
		t.Fatalf("zero product = %d", got)
	}
	if got := saturatingUintMultiply(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Fatalf("exact uint product = %d", got)
	}
	if got := saturatingUintMultiply(math.MaxUint64, 2); got != math.MaxUint64 {
		t.Fatalf("overflowing uint product = %d", got)
	}

	powers := map[uint]uint64{0: 1, 1: 3, 2: 9, 3: 27, 4: 81}
	for exponent, want := range powers {
		if got := saturatingPower(3, exponent); got != want {
			t.Fatalf("3^%d = %d, want %d", exponent, got, want)
		}
	}
}

func TestRandomDurationBoundaries(t *testing.T) {
	t.Parallel()

	random := &recordingRandom{returned: 6}
	if got := randomDuration(2, 5, random); got != 4 || random.upper != 4 {
		t.Fatalf("bounded random duration = %d with upper %d", got, random.upper)
	}
	random = &recordingRandom{returned: -1}
	if got := randomDuration(2, 5, random); got != 3 {
		t.Fatalf("negative random duration = %d", got)
	}
	random = &recordingRandom{returned: math.MaxInt64 - 1}
	if got := randomDuration(0, maxDuration, random); got != maxDuration-1 || random.upper != math.MaxInt64 {
		t.Fatalf("maximum-span duration = %d with upper %d", got, random.upper)
	}
	random = &recordingRandom{}
	if got := randomDuration(7, 7, random); got != 7 || random.upper != 0 {
		t.Fatalf("equal-bound duration = %d with upper %d", got, random.upper)
	}
	if got := randomDuration(8, 7, random); got != 8 {
		t.Fatalf("reversed-bound duration = %d", got)
	}
}

func TestBackoffBoundaryValues(t *testing.T) {
	t.Parallel()

	if got := Fibonacci(time.Second).Delay(0, 0, nil); got != time.Second {
		t.Fatalf("Fibonacci attempt zero = %s", got)
	}
	if got := Fibonacci(time.Second).Delay(3, 0, nil); got != 2*time.Second {
		t.Fatalf("Fibonacci attempt three = %s", got)
	}
	if got := ExponentialJitter(10, 1, 0).Delay(1, 0, nil); got != 10 {
		t.Fatalf("zero-factor jitter = %s", got)
	}
	if got := ExponentialJitter(10, 1, 1).Delay(1, 0, nil); got != 0 {
		t.Fatalf("full-factor jitter = %s", got)
	}
	random := &recordingRandom{}
	if got := DecorrelatedJitter(2).Delay(1, 2, random); got != 2 || random.upper != 5 {
		t.Fatalf("equal previous delay = %d with upper %d", got, random.upper)
	}
	if nonNegative(-1) != 0 || nonNegative(0) != 0 || nonNegative(1) != 1 {
		t.Fatal("non-negative normalization changed a boundary")
	}
}

func TestZeroUnitFibonacciReturnsForAnUnboundedAttempt(t *testing.T) {
	t.Parallel()

	if got := Fibonacci(0).Delay(^uint(0), 0, nil); got != 0 {
		t.Fatalf("zero-unit Fibonacci = %s", got)
	}
}

func TestPolicyBoundaryValues(t *testing.T) {
	t.Parallel()

	clock := &boundaryClock{now: time.Unix(100, 0)}
	base := Config{
		Backoff: Constant(0), MaxAttempts: 1, Clock: clock,
		Sleeper: &boundarySleeper{clock: clock}, Classifier: RetryableClassifier(),
	}
	equalDelay := base
	equalDelay.MinDelay = time.Second
	equalDelay.MaxDelay = time.Second
	if _, err := NewPolicy(equalDelay); err != nil {
		t.Fatalf("equal delay bounds: %v", err)
	}
	unboundedMaximum := base
	unboundedMaximum.MinDelay = time.Second
	if _, err := NewPolicy(unboundedMaximum); err != nil {
		t.Fatalf("zero maximum delay: %v", err)
	}
	maximumHistory := base
	maximumHistory.HistoryLimit = MaxHistoryEntries
	if _, err := NewPolicy(maximumHistory); err != nil {
		t.Fatalf("maximum history: %v", err)
	}

	policy, err := NewPolicy(equalDelay)
	if err != nil {
		t.Fatal(err)
	}
	for delay, want := range map[time.Duration]time.Duration{-1: time.Second, time.Second: time.Second, 2 * time.Second: time.Second} {
		if got := policy.boundDelay(delay); got != want {
			t.Fatalf("boundDelay(%s) = %s, want %s", delay, got, want)
		}
	}
}

func TestNilLikeKinds(t *testing.T) {
	t.Parallel()

	var channel chan struct{}
	var function func()
	var mapping map[string]string
	var pointer *int
	var slice []byte
	for _, value := range []any{nil, channel, function, mapping, pointer, slice} {
		if !nilLike(value) {
			t.Fatalf("%T was not recognized as nil-like", value)
		}
	}
	for _, value := range []any{0, make(chan struct{}), func() {}, map[string]string{}, new(int), []byte{}} {
		if nilLike(value) {
			t.Fatalf("%T was recognized as nil-like", value)
		}
	}
}

func TestAttemptContextBudgetBoundaries(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	clock := &boundaryClock{now: start}
	policy := &Policy{config: Config{Clock: clock, MaxElapsed: 10 * time.Second, AttemptTimeout: 5 * time.Second}}
	ctx, cancel, kind := policy.attemptContext(context.Background(), start)
	cancel()
	if kind != BudgetAttempt || clock.timeout != 5*time.Second || ctx == nil {
		t.Fatalf("attempt budget = %s with timeout %s", kind, clock.timeout)
	}
	clock.now = start.Add(5 * time.Second)
	_, cancel, kind = policy.attemptContext(context.Background(), start)
	cancel()
	if kind != BudgetElapsed || clock.timeout != 5*time.Second {
		t.Fatalf("equal elapsed budget = %s with timeout %s", kind, clock.timeout)
	}
	policy.config.AttemptTimeout = 0
	clock.now = start
	_, cancel, kind = policy.attemptContext(context.Background(), start)
	cancel()
	if kind != BudgetElapsed || clock.timeout != 10*time.Second {
		t.Fatalf("elapsed-only budget = %s with timeout %s", kind, clock.timeout)
	}
	policy.config.MaxElapsed = 0
	parent := context.Background()
	ctx, cancel, kind = policy.attemptContext(parent, start)
	cancel()
	if ctx != parent || kind != BudgetAttempt {
		t.Fatalf("unbounded attempt context = (%v, %s)", ctx, kind)
	}
}

func TestExactElapsedAndSleepBudgets(t *testing.T) {
	t.Parallel()

	t.Run("elapsed", func(t *testing.T) {
		clock := &boundaryClock{now: time.Unix(100, 0)}
		sleeper := &boundarySleeper{clock: clock}
		policy := mustBoundaryPolicy(t, Config{
			Backoff: Constant(time.Second), MaxAttempts: 4, MaxElapsed: 2 * time.Second,
			Clock: clock, Sleeper: sleeper, Classifier: RetryableClassifier(),
		})
		calls := 0
		_, result, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
			calls++
			return struct{}{}, Retryable(errors.New("temporary"))
		})
		var budget *BudgetError
		if !errors.As(err, &budget) || budget.Kind != BudgetElapsed || calls != 2 || len(sleeper.delays) != 2 || result.Attempts != 2 {
			t.Fatalf("elapsed boundary: calls=%d sleeps=%v result=%+v error=%v", calls, sleeper.delays, result, err)
		}
	})

	t.Run("sleep", func(t *testing.T) {
		clock := &boundaryClock{now: time.Unix(100, 0)}
		sleeper := &boundarySleeper{clock: clock}
		policy := mustBoundaryPolicy(t, Config{
			Backoff: Constant(time.Second), MaxAttempts: 4, MaxSleep: 2 * time.Second,
			Clock: clock, Sleeper: sleeper, Classifier: RetryableClassifier(),
		})
		calls := 0
		_, result, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
			calls++
			return struct{}{}, Retryable(errors.New("temporary"))
		})
		var budget *BudgetError
		if !errors.As(err, &budget) || budget.Kind != BudgetSleep || calls != 3 || len(sleeper.delays) != 2 || result.Attempts != 3 {
			t.Fatalf("sleep boundary: calls=%d sleeps=%v result=%+v error=%v", calls, sleeper.delays, result, err)
		}
	})
}

func TestAttemptErrorMustBeDeadlineExceeded(t *testing.T) {
	t.Parallel()

	clock := &boundaryClock{now: time.Unix(100, 0), contextErr: errors.New("attempt context warning")}
	policy := mustBoundaryPolicy(t, Config{
		Backoff: Constant(0), MaxAttempts: 1, AttemptTimeout: time.Second,
		Clock: clock, Sleeper: &boundarySleeper{clock: clock}, Classifier: RetryableClassifier(),
	})
	_, result, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		return struct{}{}, Retryable(errors.New("temporary"))
	})
	var exhausted *ExhaustedError
	if !errors.As(err, &exhausted) || result.Reason != ReasonAttemptsExhausted {
		t.Fatalf("non-deadline attempt error: result=%+v error=%v", result, err)
	}
}

func TestUnavailableDelayHintIsIgnored(t *testing.T) {
	t.Parallel()

	clock := &boundaryClock{now: time.Unix(100, 0)}
	sleeper := &boundarySleeper{clock: clock}
	policy := mustBoundaryPolicy(t, Config{
		Backoff: Constant(time.Second), MaxAttempts: 2,
		Clock: clock, Sleeper: sleeper,
		Classifier: ClassifyFunc(func(context.Context, error) (Classification, error) {
			return ClassificationRetryable, nil
		}),
	})
	calls := 0
	_, _, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		calls++
		if calls == 1 {
			return struct{}{}, boundaryHint{delay: 10 * time.Second, available: false}
		}
		return struct{}{}, nil
	})
	if err != nil || len(sleeper.delays) != 1 || sleeper.delays[0] != time.Second {
		t.Fatalf("unavailable hint: sleeps=%v error=%v", sleeper.delays, err)
	}
}

func TestBudgetArithmeticBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("elapsed equality", func(t *testing.T) {
		clock := &boundaryClock{now: time.Unix(100, 0)}
		sleeper := &boundarySleeper{clock: clock}
		policy := mustBoundaryPolicy(t, Config{
			Backoff: Constant(0), MaxAttempts: 2, MaxElapsed: 2 * time.Second,
			Clock: clock, Sleeper: sleeper, Classifier: RetryableClassifier(),
		})
		_, _, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
			clock.now = clock.now.Add(2 * time.Second)
			return struct{}{}, Retryable(errors.New("temporary"))
		})
		var budget *BudgetError
		if !errors.As(err, &budget) || budget.Kind != BudgetElapsed || len(sleeper.delays) != 0 {
			t.Fatalf("elapsed equality: sleeps=%v error=%v", sleeper.delays, err)
		}
	})

	t.Run("elapsed subtraction", func(t *testing.T) {
		clock := &boundaryClock{now: time.Unix(100, 0)}
		sleeper := &boundarySleeper{clock: clock}
		policy := mustBoundaryPolicy(t, Config{
			Backoff: Constant(7 * time.Second), MaxAttempts: 2, MaxElapsed: 10 * time.Second,
			Clock: clock, Sleeper: sleeper, Classifier: RetryableClassifier(),
		})
		_, _, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
			clock.now = clock.now.Add(4 * time.Second)
			return struct{}{}, Retryable(errors.New("temporary"))
		})
		var budget *BudgetError
		if !errors.As(err, &budget) || budget.Kind != BudgetElapsed || len(sleeper.delays) != 0 {
			t.Fatalf("elapsed subtraction: sleeps=%v error=%v", sleeper.delays, err)
		}
	})

	t.Run("sleep equality", func(t *testing.T) {
		clock := &boundaryClock{now: time.Unix(100, 0)}
		sleeper := &boundarySleeper{clock: clock}
		policy := mustBoundaryPolicy(t, Config{
			Backoff:     &sequenceBackoff{delays: []time.Duration{time.Second, 0}},
			MaxAttempts: 3, MaxSleep: time.Second,
			Clock: clock, Sleeper: sleeper, Classifier: RetryableClassifier(),
		})
		_, _, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
			return struct{}{}, Retryable(errors.New("temporary"))
		})
		var budget *BudgetError
		if !errors.As(err, &budget) || budget.Kind != BudgetSleep || len(sleeper.delays) != 1 {
			t.Fatalf("sleep equality: sleeps=%v error=%v", sleeper.delays, err)
		}
	})

	t.Run("sleep subtraction", func(t *testing.T) {
		clock := &boundaryClock{now: time.Unix(100, 0)}
		sleeper := &boundarySleeper{clock: clock}
		policy := mustBoundaryPolicy(t, Config{
			Backoff:     &sequenceBackoff{delays: []time.Duration{4 * time.Second, 7 * time.Second}},
			MaxAttempts: 3, MaxSleep: 10 * time.Second,
			Clock: clock, Sleeper: sleeper, Classifier: RetryableClassifier(),
		})
		_, _, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
			return struct{}{}, Retryable(errors.New("temporary"))
		})
		var budget *BudgetError
		if !errors.As(err, &budget) || budget.Kind != BudgetSleep || len(sleeper.delays) != 1 {
			t.Fatalf("sleep subtraction: sleeps=%v error=%v", sleeper.delays, err)
		}
	})
}

func TestObserverIsInvoked(t *testing.T) {
	t.Parallel()

	calls := 0
	policy := &Policy{config: Config{Observer: ObserveFunc(func(Observation) { calls++ })}}
	observe(policy, Observation{Reason: ReasonSucceeded})
	if calls != 1 {
		t.Fatalf("observer calls = %d", calls)
	}
}

func TestDoRejectsConstructedZeroAttemptPolicy(t *testing.T) {
	t.Parallel()

	clock := &boundaryClock{now: time.Unix(100, 0)}
	policy := &Policy{config: Config{
		Backoff: Constant(0), Clock: clock,
		Sleeper: &boundarySleeper{clock: clock}, Classifier: RetryableClassifier(),
	}}
	_, _, err := Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("zero-attempt policy error = %v", err)
	}
}

func mustBoundaryPolicy(t *testing.T, config Config) *Policy {
	t.Helper()
	policy, err := NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

type recordingRandom struct {
	returned int64
	upper    int64
}

type sequenceBackoff struct {
	delays []time.Duration
	index  int
}

func (backoff *sequenceBackoff) Delay(uint, time.Duration, Random) time.Duration {
	delay := backoff.delays[backoff.index]
	backoff.index++
	return delay
}

func (random *recordingRandom) Int64n(upper int64) int64 {
	random.upper = upper
	return random.returned
}

type boundaryClock struct {
	now        time.Time
	timeout    time.Duration
	contextErr error
}

func (clock *boundaryClock) Now() time.Time { return clock.now }

func (clock *boundaryClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	clock.timeout = timeout
	if clock.contextErr != nil {
		return boundaryErrorContext{Context: parent, err: clock.contextErr}, func() {}
	}
	return context.WithCancel(parent)
}

type boundaryErrorContext struct {
	context.Context
	err error
}

func (ctx boundaryErrorContext) Err() error { return ctx.err }

type boundaryHint struct {
	delay     time.Duration
	available bool
}

func (hint boundaryHint) Error() string { return "hint" }

func (hint boundaryHint) RetryDelay(time.Time) (time.Duration, bool) {
	return hint.delay, hint.available
}

type boundarySleeper struct {
	clock  *boundaryClock
	delays []time.Duration
}

func (sleeper *boundarySleeper) Sleep(_ context.Context, delay time.Duration) error {
	sleeper.delays = append(sleeper.delays, delay)
	sleeper.clock.now = sleeper.clock.now.Add(delay)
	return nil
}
