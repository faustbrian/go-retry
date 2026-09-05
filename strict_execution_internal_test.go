package retry

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvokeStrictOperationCompletesPermitExactlyOnceAndIgnoresError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("completion failed")
	var events []string
	permit := &strictPermitProbe{complete: func() error {
		events = append(events, "complete")
		return wantErr
	}}
	ctx := context.WithValue(context.Background(), strictInternalContextKey{}, "identity")
	wantResult := AttemptResult[string]{Value: "done", Outcome: OutcomeKnown}
	result, err := invokeStrictOperation(ctx, func(got context.Context) (AttemptResult[string], error) {
		events = append(events, "operation")
		if got != ctx {
			t.Fatal("operation context identity changed")
		}
		return wantResult, nil
	}, permit)
	if err != nil || result != wantResult || permit.calls != 1 || !reflect.DeepEqual(events, []string{"operation", "complete"}) {
		t.Fatalf("result=%+v err=%v calls=%d events=%v", result, err, permit.calls, events)
	}
}

func TestInvokeStrictOperationCompletesPermitDuringPanic(t *testing.T) {
	t.Parallel()

	want := errors.New("operation panic")
	permit := &strictPermitProbe{}
	defer func() {
		//nolint:errorlint // Panic value identity is part of the contract.
		if recovered := recover(); recovered != want || permit.calls != 1 {
			t.Fatalf("recovered=%v calls=%d", recovered, permit.calls)
		}
	}()
	_, _ = invokeStrictOperation(context.Background(), func(context.Context) (AttemptResult[string], error) {
		panic(want)
	}, permit)
}

func TestInvokeStrictOperationPropagatesPermitPanic(t *testing.T) {
	t.Parallel()

	want := errors.New("permit panic")
	permit := &strictPermitProbe{complete: func() error { panic(want) }}
	defer func() {
		//nolint:errorlint // Panic value identity is part of the contract.
		if recovered := recover(); recovered != want || permit.calls != 1 {
			t.Fatalf("recovered=%v calls=%d", recovered, permit.calls)
		}
	}()
	_, _ = invokeStrictOperation(context.Background(), func(context.Context) (AttemptResult[string], error) {
		return AttemptResult[string]{Outcome: OutcomeKnown}, nil
	}, permit)
}

func TestInvokeStrictOperationPermitExecutionMechanics(t *testing.T) {
	t.Run("blocking and concurrent", func(t *testing.T) {
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		permit := &strictConcurrentPermitProbe{complete: func() error {
			entered <- struct{}{}
			<-release
			return nil
		}}
		done := make(chan struct{}, 2)
		for range 2 {
			go func() {
				_, _ = invokeStrictOperation(context.Background(), func(context.Context) (AttemptResult[string], error) {
					return AttemptResult[string]{Value: "done", Outcome: OutcomeKnown}, nil
				}, permit)
				done <- struct{}{}
			}()
		}
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("permit completion was not concurrent")
			}
		}
		select {
		case <-done:
			close(release)
			t.Fatal("operation returned while completion blocked")
		default:
		}
		close(release)
		for range 2 {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("operation did not return after completion release")
			}
		}
		if permit.calls.Load() != 2 {
			t.Fatalf("completion calls = %d", permit.calls.Load())
		}
	})

	t.Run("reentrant", func(t *testing.T) {
		var permit *strictConcurrentPermitProbe
		reentered := false
		permit = &strictConcurrentPermitProbe{complete: func() error {
			if reentered {
				return nil
			}
			reentered = true
			_, err := invokeStrictOperation(context.Background(), func(context.Context) (AttemptResult[string], error) {
				return AttemptResult[string]{Value: "inner", Outcome: OutcomeKnown}, nil
			}, permit)
			return err
		}}
		result, err := invokeStrictOperation(context.Background(), func(context.Context) (AttemptResult[string], error) {
			return AttemptResult[string]{Value: "outer", Outcome: OutcomeKnown}, nil
		}, permit)
		if err != nil || result.Value != "outer" || !reentered || permit.calls.Load() != 2 {
			t.Fatalf("result=%+v err=%v reentered=%v calls=%d", result, err, reentered, permit.calls.Load())
		}
	})

	t.Run("cancellation does not skip completion", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		permit := &strictConcurrentPermitProbe{}
		result, err := invokeStrictOperation(ctx, func(got context.Context) (AttemptResult[string], error) {
			if got != ctx {
				t.Fatal("operation context identity changed")
			}
			cancel()
			return AttemptResult[string]{Value: "known", Outcome: OutcomeKnown}, nil
		}, permit)
		if err != nil || result.Value != "known" || ctx.Err() != context.Canceled || permit.calls.Load() != 1 {
			t.Fatalf("result=%+v err=%v ctx=%v calls=%d", result, err, ctx.Err(), permit.calls.Load())
		}
	})
}

type strictInternalContextKey struct{}

type strictPermitProbe struct {
	calls    int
	complete func() error
}

type strictConcurrentPermitProbe struct {
	calls    atomic.Int32
	complete func() error
}

func (permit *strictConcurrentPermitProbe) Complete() error {
	permit.calls.Add(1)
	if permit.complete != nil {
		return permit.complete()
	}
	return nil
}

func (permit *strictPermitProbe) Complete() error {
	permit.calls++
	if permit.complete != nil {
		return permit.complete()
	}
	return nil
}
