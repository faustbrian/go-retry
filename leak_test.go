package retry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
	"go.uber.org/goleak"
)

func TestMain(main *testing.M) {
	goleak.VerifyTestMain(main, goleak.IgnoreCurrent())
}

func TestNoPackageBackgroundWorkers(t *testing.T) {
	t.Parallel()

	const workers = 64
	entered := make(chan struct{}, workers)
	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.Constant(time.Hour), MaxAttempts: 2,
		Clock: retry.SystemClock{}, Sleeper: signalingSleeper{entered: entered},
		Classifier: retry.RetryableClassifier(),
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	cancels := make([]context.CancelFunc, 0, workers)
	errorsByWorker := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancels = append(cancels, cancel)
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, executionErr := retry.Do(ctx, policy, alwaysRetryable)
			errorsByWorker <- executionErr
		}()
	}
	for range workers {
		<-entered
	}
	for _, cancel := range cancels {
		cancel()
	}
	wait.Wait()
	close(errorsByWorker)
	for executionErr := range errorsByWorker {
		if !errors.Is(executionErr, context.Canceled) {
			t.Fatalf("Do error = %v, want context cancellation", executionErr)
		}
	}
}

type signalingSleeper struct{ entered chan<- struct{} }

func (sleeper signalingSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	sleeper.entered <- struct{}{}
	return (retry.SystemSleeper{}).Sleep(ctx, delay)
}
