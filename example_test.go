package retry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func ExampleDo() {
	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 3,
		Clock: retry.SystemClock{}, Sleeper: retry.SystemSleeper{},
		Classifier: retry.RetryableClassifier(), HistoryLimit: 2,
	})
	if err != nil {
		panic(err)
	}
	attempts := 0
	value, result, err := retry.Do(context.Background(), policy, func(context.Context) (string, error) {
		attempts++
		if attempts == 1 {
			return "", retry.Retryable(errors.New("temporary"))
		}
		return "ready", nil
	})
	fmt.Println(value, result.Attempts, err)
	// Output: ready 2 <nil>
}

func ExampleFullJitter() {
	strategy := retry.FullJitter(retry.Exponential(100*time.Millisecond, 2))
	delay := strategy.Delay(1, 0, retry.NewRandom(1, 2))
	fmt.Println(delay >= 0 && delay <= 100*time.Millisecond)
	// Output: true
}
