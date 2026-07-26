package retry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func TestPolicyIsSafeForConcurrentExecution(t *testing.T) {
	t.Parallel()

	policy := mustPolicy(t, retry.Config{
		Backoff: retry.FullJitter(retry.Constant(time.Second)), MaxAttempts: 2,
		Clock: fixedTimeClock{time.Unix(100, 0)}, Sleeper: noOpSleeper{},
		Random: retry.NewRandom(1, 2), Classifier: retry.RetryableClassifier(),
	})
	var wait sync.WaitGroup
	for range 64 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			calls := 0
			_, _, err := retry.Do(context.Background(), policy, func(context.Context) (struct{}, error) {
				calls++
				if calls == 1 {
					return struct{}{}, retry.Retryable(errors.New("temporary"))
				}
				return struct{}{}, nil
			})
			if err != nil {
				t.Errorf("Do: %v", err)
			}
		}()
	}
	wait.Wait()
}

type fixedTimeClock struct{ now time.Time }

func (clock fixedTimeClock) Now() time.Time { return clock.now }

type noOpSleeper struct{}

func (noOpSleeper) Sleep(context.Context, time.Duration) error { return nil }
