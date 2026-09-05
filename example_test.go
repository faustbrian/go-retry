package retry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	retry "github.com/faustbrian/go-retry"
)

func ExampleDoStrict() {
	policy, err := retry.NewPolicyStrict(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 3,
		Clock: retry.SystemClock{}, Sleeper: retry.SystemSleeper{},
		Classifier: retry.RetryableClassifier(), HistoryLimit: 2,
	})
	if err != nil {
		panic(err)
	}
	attempts := 0
	result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		attempts++
		if attempts == 1 {
			return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(errors.New("temporary"))
		}
		return retry.AttemptResult[string]{Value: "ready", Outcome: retry.OutcomeKnown}, nil
	})
	fmt.Println(result.Value, result.Retry.Attempts, err)
	// Output: ready 2 <nil>
}

func ExampleFullJitter() {
	strategy := retry.FullJitter(retry.Exponential(100*time.Millisecond, 2))
	delay := strategy.Delay(1, 0, retry.NewRandom(1, 2))
	fmt.Println(delay >= 0 && delay <= 100*time.Millisecond)
	// Output: true
}
