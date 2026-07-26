package retry_test

import (
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func FuzzBackoffNeverProducesNegativeDelay(fuzz *testing.F) {
	fuzz.Add(uint(1), int64(time.Second), uint64(2), uint(2))
	fuzz.Add(^uint(0), int64(1<<63-1), ^uint64(0), uint(63))
	fuzz.Fuzz(func(t *testing.T, attempt uint, nanoseconds int64, multiplier uint64, power uint) {
		if power > 128 {
			power %= 129
		}
		strategies := []retry.Backoff{
			retry.Constant(time.Duration(nanoseconds)),
			retry.Linear(time.Duration(nanoseconds), time.Duration(nanoseconds)),
			retry.Polynomial(time.Duration(nanoseconds), time.Duration(nanoseconds), power),
			retry.Fibonacci(time.Duration(nanoseconds)),
			retry.Exponential(time.Duration(nanoseconds), multiplier),
		}
		for _, strategy := range strategies {
			if delay := strategy.Delay(attempt, time.Duration(nanoseconds), nil); delay < 0 {
				t.Fatalf("negative delay %s", delay)
			}
		}
	})
}

func FuzzPolicyValidationDoesNotAcceptContradictoryDelayBounds(fuzz *testing.F) {
	fuzz.Add(int64(time.Second), int64(2*time.Second), uint(3))
	fuzz.Fuzz(func(t *testing.T, minimum, maximum int64, attempts uint) {
		policy, err := retry.NewPolicy(retry.Config{
			Backoff: retry.Constant(0), MaxAttempts: attempts,
			MinDelay: time.Duration(minimum), MaxDelay: time.Duration(maximum),
			Clock: retry.SystemClock{}, Sleeper: retry.SystemSleeper{},
			Classifier: retry.RetryableClassifier(),
		})
		if err == nil && (policy == nil || attempts == 0 || minimum < 0 || maximum < 0 || maximum > 0 && minimum > maximum) {
			t.Fatalf("invalid policy accepted: minimum=%d maximum=%d attempts=%d", minimum, maximum, attempts)
		}
	})
}
