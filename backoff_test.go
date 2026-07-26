package retry_test

import (
	"math"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func TestDeterministicBackoffVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backoff  retry.Backoff
		expected []time.Duration
	}{
		{"constant", retry.Constant(time.Second), []time.Duration{time.Second, time.Second, time.Second, time.Second, time.Second}},
		{"linear", retry.Linear(time.Second, 2*time.Second), []time.Duration{time.Second, 3 * time.Second, 5 * time.Second, 7 * time.Second, 9 * time.Second}},
		{"polynomial", retry.Polynomial(time.Second, time.Second, 2), []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 17 * time.Second, 26 * time.Second}},
		{"fibonacci", retry.Fibonacci(time.Second), []time.Duration{time.Second, time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second}},
		{"exponential", retry.Exponential(time.Second, 2), []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for index, expected := range test.expected {
				attempt := uint(index + 1)
				if actual := test.backoff.Delay(attempt, 0, nil); actual != expected {
					t.Fatalf("attempt %d: got %s, want %s", attempt, actual, expected)
				}
			}
		})
	}
}

func TestBackoffsSaturateInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	backoffs := []retry.Backoff{
		retry.Linear(time.Duration(math.MaxInt64-1), time.Second),
		retry.Polynomial(time.Duration(math.MaxInt64-1), time.Second, 63),
		retry.Fibonacci(time.Duration(math.MaxInt64)),
		retry.Exponential(time.Duration(math.MaxInt64), math.MaxUint64),
	}

	for _, backoff := range backoffs {
		if delay := backoff.Delay(1000, 0, nil); delay != time.Duration(math.MaxInt64) {
			t.Fatalf("got %s, want saturation at %s", delay, time.Duration(math.MaxInt64))
		}
	}
}

func TestZeroFibonacciNeverBecomesMaximumDelay(t *testing.T) {
	t.Parallel()

	if delay := retry.Fibonacci(0).Delay(1000, 0, nil); delay != 0 {
		t.Fatalf("zero Fibonacci delay = %s, want zero", delay)
	}
}

func TestJitterStrategiesUseInjectedRandomnessWithinBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backoff  retry.Backoff
		previous time.Duration
		random   int64
		want     time.Duration
		minimum  time.Duration
		maximum  time.Duration
	}{
		{"full", retry.FullJitter(retry.Constant(10 * time.Second)), 0, int64(3 * time.Second), 3 * time.Second, 0, 10 * time.Second},
		{"equal", retry.EqualJitter(retry.Constant(10 * time.Second)), 0, int64(3 * time.Second), 8 * time.Second, 5 * time.Second, 10 * time.Second},
		{"exponential", retry.ExponentialJitter(time.Second, 2, 0.25), 0, 0, 750 * time.Millisecond, 750 * time.Millisecond, 1250 * time.Millisecond},
		{"decorrelated", retry.DecorrelatedJitter(time.Second), 2 * time.Second, int64(3 * time.Second), 4 * time.Second, time.Second, 6 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := test.backoff.Delay(1, test.previous, fixedRandom(test.random))
			if actual != test.want {
				t.Fatalf("got %s, want %s", actual, test.want)
			}
			for value := uint64(0); value < 1000; value++ {
				delay := test.backoff.Delay(1, test.previous, fixedRandom(value))
				if delay < test.minimum || delay > test.maximum {
					t.Fatalf("random %d produced out-of-range delay %s", value, delay)
				}
			}
		})
	}
}

func TestJitterWithoutRandomSourceUsesLowerBound(t *testing.T) {
	t.Parallel()

	if delay := retry.FullJitter(retry.Constant(time.Second)).Delay(1, 0, nil); delay != 0 {
		t.Fatalf("full jitter without source = %s, want zero", delay)
	}
	if delay := retry.DecorrelatedJitter(time.Second).Delay(1, 0, nil); delay != time.Second {
		t.Fatalf("decorrelated jitter without source = %s, want one second", delay)
	}
}

type fixedRandom int64

func (random fixedRandom) Int64n(upper int64) int64 {
	if upper <= 0 {
		return 0
	}
	return int64(random) % upper
}
