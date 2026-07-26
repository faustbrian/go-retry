package retry_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func TestSystemSleeperHonorsCanceledContextWithoutWaiting(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (retry.SystemSleeper{}).Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep error = %v, want context cancellation", err)
	}
}

func TestSystemSleeperCompletesTimerAndInFlightCancellation(t *testing.T) {
	if err := (retry.SystemSleeper{}).Sleep(context.Background(), 0); err != nil {
		t.Fatalf("zero sleep error = %v", err)
	}
	if err := (retry.SystemSleeper{}).Sleep(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("timer sleep error = %v", err)
	}
	ctx := newStagedCanceledContext()
	if err := (retry.SystemSleeper{}).Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation error = %v", err)
	}
}

func TestSeededRandomIsDeterministicAndBounded(t *testing.T) {
	t.Parallel()

	left := retry.NewRandom(1, 2)
	right := retry.NewRandom(1, 2)
	for range 1000 {
		leftValue, rightValue := left.Int64n(17), right.Int64n(17)
		if leftValue != rightValue || leftValue >= 17 {
			t.Fatalf("values = (%d, %d), want equal values below 17", leftValue, rightValue)
		}
	}
	if value := left.Int64n(0); value != 0 {
		t.Fatalf("Int64n(0) = %d, want zero", value)
	}
}

func TestSystemClockCreatesAttemptDeadline(t *testing.T) {
	t.Parallel()

	clock := retry.SystemClock{}
	ctx, cancel := clock.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("context error = %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("attempt context did not expire")
	}
	if clock.Now().IsZero() {
		t.Fatal("system clock returned zero time")
	}
}

type stagedCanceledContext struct {
	context.Context
	done  chan struct{}
	calls atomic.Uint32
}

func newStagedCanceledContext() *stagedCanceledContext {
	done := make(chan struct{})
	close(done)
	return &stagedCanceledContext{Context: context.Background(), done: done}
}

func (ctx *stagedCanceledContext) Done() <-chan struct{} { return ctx.done }

func (ctx *stagedCanceledContext) Err() error {
	if ctx.calls.Add(1) == 1 {
		return nil
	}
	return context.Canceled
}
