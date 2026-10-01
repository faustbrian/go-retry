package retry_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v2"
)

func TestDefaultMarkersDoNotFormatPrivateCause(t *testing.T) {
	for _, mark := range []struct {
		name string
		mark func(error) error
	}{{"retryable", retry.Retryable}, {"permanent", retry.Permanent}} {
		t.Run(mark.name, func(t *testing.T) {
			cause := &privateDefaultCause{}
			err := mark.mark(cause)
			assertDefaultPrivateError(t, err, cause, mark.name)
		})
	}
}

func TestDefaultDoTerminalErrorsDoNotFormatPrivateCause(t *testing.T) {
	for _, name := range []string{"permanent", "exhausted", "canceled", "sleep", "invalid classifier", "panicking classifier"} {
		t.Run(name, func(t *testing.T) {
			cause := &privateDefaultCause{}
			clock := newManualClock(time.Unix(700, 0))
			config := baseConfig(clock, retry.Config{})
			config.MaxAttempts = 1
			ctx := context.Background()
			operationErr := error(cause)
			wantReason := retry.ReasonPermanent
			category := name
			switch name {
			case "exhausted":
				operationErr = retry.Retryable(cause)
				wantReason = retry.ReasonAttemptsExhausted
			case "canceled":
				ctx = privateDefaultContext{Context: ctx, err: cause}
				wantReason = retry.ReasonCanceled
			case "sleep":
				config.MaxAttempts = 2
				config.MaxSleep = time.Second
				config.Backoff = retry.Constant(2 * time.Second)
				operationErr = retry.Retryable(cause)
				wantReason = retry.ReasonSleepBudget
			case "invalid classifier":
				config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { return 99, nil })
				wantReason = retry.ReasonClassifierFailure
				category = "permanent"
			case "panicking classifier":
				config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { panic(cause) })
				wantReason = retry.ReasonClassifierFailure
				category = "permanent"
			}
			policy := mustPolicy(t, config)
			calls := 0
			value, result, err := retry.Do(ctx, policy, func(context.Context) (string, error) {
				calls++
				return "", operationErr
			})
			if value != "" || result.Reason != wantReason || (name == "canceled" && calls != 0) || (name != "canceled" && calls != 1) {
				t.Fatalf("value=%q reason=%q calls=%d", value, result.Reason, calls)
			}
			assertDefaultPrivateError(t, err, cause, category)
		})
	}
}

func TestBudgetErrorDoesNotRenderCallerSuppliedKind(t *testing.T) {
	err := &retry.BudgetError{Kind: retry.BudgetKind("application-private")}
	text := err.Error()
	if strings.Contains(text, "application-private") || !strings.Contains(text, "budget") {
		t.Fatalf("unsafe budget category %q", text)
	}
}

func TestDoClassifierPanicCauseIsExplicitMachineDiagnostic(t *testing.T) {
	operationCause := errors.New("operation")
	panicCause := &privateDefaultCause{}
	clock := newManualClock(time.Unix(710, 0))
	config := baseConfig(clock, retry.Config{})
	config.Classifier = retry.ClassifyFunc(func(context.Context, error) (retry.Classification, error) { panic(panicCause) })
	_, result, err := retry.Do(context.Background(), mustPolicy(t, config), func(context.Context) (string, error) { return "", operationCause })
	if result.Reason != retry.ReasonClassifierFailure || !errors.Is(err, operationCause) {
		t.Fatal("classifier failure lost operation classification")
	}
	assertDefaultPrivateError(t, err, panicCause, "permanent")
}

type privateDefaultCause struct{ calls int }

type privateDefaultContext struct {
	context.Context
	err error
}

func (ctx privateDefaultContext) Err() error { return ctx.err }

func (cause *privateDefaultCause) Error() string {
	cause.calls++
	return "application-private"
}

func assertDefaultPrivateError(t *testing.T, err error, cause *privateDefaultCause, category string) {
	t.Helper()
	if err == nil {
		t.Fatal("missing terminal error")
	}
	text := err.Error()
	var recovered *privateDefaultCause
	if cause.calls != 0 || strings.Contains(text, "application-private") || !strings.Contains(text, category) || len(text) > 66 || !errors.Is(err, cause) || !errors.As(err, &recovered) || recovered != cause {
		t.Fatalf("category=%q cause calls=%d preserved=%t", text, cause.calls, recovered == cause)
	}
}
