package retry_test

import (
	"context"
	"testing"

	avastretry "github.com/avast/retry-go/v4"
	backoff "github.com/cenkalti/backoff/v5"
	retry "github.com/faustbrian/golib/pkg/retry"
)

func BenchmarkSuccessfulExecution(b *testing.B) {
	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1,
		Clock: retry.SystemClock{}, Sleeper: retry.SystemSleeper{},
		Classifier: retry.RetryableClassifier(),
	})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.Run("retry", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _, _ = retry.Do(ctx, policy, func(context.Context) (struct{}, error) { return struct{}{}, nil })
		}
	})
	b.Run("cenkalti-backoff-v5", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = backoff.Retry(ctx, func() (struct{}, error) { return struct{}{}, nil }, backoff.WithMaxTries(1))
		}
	})
	b.Run("avast-retry-go-v4", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = avastretry.DoWithData(func() (struct{}, error) { return struct{}{}, nil }, avastretry.Attempts(1))
		}
	})
}

func BenchmarkBackoffStrategies(b *testing.B) {
	strategies := map[string]retry.Backoff{
		"constant": retry.Constant(100), "linear": retry.Linear(100, 100),
		"polynomial": retry.Polynomial(100, 100, 2), "fibonacci": retry.Fibonacci(100),
		"exponential":         retry.Exponential(100, 2),
		"full-jitter":         retry.FullJitter(retry.Exponential(100, 2)),
		"equal-jitter":        retry.EqualJitter(retry.Exponential(100, 2)),
		"exponential-jitter":  retry.ExponentialJitter(100, 2, 0.25),
		"decorrelated-jitter": retry.DecorrelatedJitter(100),
	}
	for name, strategy := range strategies {
		b.Run(name, func(b *testing.B) {
			random := retry.NewRandom(1, 2)
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				_ = strategy.Delay(uint(index%32+1), 100, random)
			}
		})
	}
}
