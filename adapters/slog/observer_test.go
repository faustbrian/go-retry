package retryslog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
	retryslog "github.com/faustbrian/go-retry/adapters/slog"

	//lint:ignore SA1019 Legacy parity is the compatibility contract under test.
	legacy "github.com/faustbrian/go-retry/retrylog" //nolint:staticcheck // Legacy parity is the compatibility contract under test.
)

func TestNewValidatesLoggerAndPolicyID(t *testing.T) {
	t.Parallel()

	if observer, err := retryslog.New(retryslog.Options{}); observer != nil || err == nil || err.Error() != "invalid retry policy: logger is required" || !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("missing logger = (%v,%v)", observer, err)
	}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	if _, err := retryslog.New(retryslog.Options{Logger: logger, PolicyID: strings.Repeat("x", retryslog.MaxPolicyIDLength)}); err != nil {
		t.Fatalf("maximum policy ID: %v", err)
	}
	if observer, err := retryslog.New(retryslog.Options{Logger: logger, PolicyID: strings.Repeat("x", retryslog.MaxPolicyIDLength+1)}); observer != nil || err == nil || err.Error() != "invalid retry policy: policy ID exceeds 128 bytes" || !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("oversized policy ID = (%v,%v)", observer, err)
	}
}

func TestObserverLogsExactBoundedFieldsAndOutcomeUnknown(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	observer, err := retryslog.New(retryslog.Options{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
		Level:  slog.LevelInfo, PolicyID: "invoice-read",
	})
	if err != nil {
		t.Fatal(err)
	}
	observer.Observe(retry.Observation{
		Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second,
		Classification: retry.ClassificationRetryable, Reason: retry.ReasonOutcomeUnknown,
	})
	log := output.String()
	for _, field := range []string{"invoice-read", `"attempt":2`, `"elapsed_ns":3000000000`, `"next_delay_ns":1000000000`, `"classification":"retryable"`, `"reason":"outcome_unknown"`} {
		if !strings.Contains(log, field) {
			t.Errorf("log %q lacks %q", log, field)
		}
	}
	if strings.Contains(log, "secret") || strings.Contains(log, "error") {
		t.Fatalf("log contains unbounded payload: %q", log)
	}
}

func TestObserverZeroAndNilReceiversAreNoOps(t *testing.T) {
	t.Parallel()

	for _, observer := range []*retryslog.Observer{{}, nil} {
		observer.Observe(retry.Observation{Reason: retry.ReasonOutcomeUnknown})
	}
}

func TestObserverBoundsEveryClassificationAndReason(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	observer, err := retryslog.New(retryslog.Options{Logger: slog.New(slog.NewJSONHandler(&output, nil))})
	if err != nil {
		t.Fatal(err)
	}
	for _, classification := range []retry.Classification{0, retry.ClassificationPermanent, retry.ClassificationRetryable, 99} {
		observer.Observe(retry.Observation{Classification: classification})
	}
	for _, reason := range []retry.Reason{"", retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted,
		retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget, retry.ReasonAttemptBudget,
		retry.ReasonClassifierFailure, retry.ReasonSleeperFailure, retry.ReasonWorkBudget, retry.ReasonOutcomeUnknown, "hostile"} {
		observer.Observe(retry.Observation{Reason: reason})
	}
	if !strings.Contains(output.String(), `"classification":"unknown"`) || !strings.Contains(output.String(), `"reason":"unknown"`) {
		t.Fatalf("bounded mappings missing: %s", output.String())
	}
}

func TestObserverMappingsMatchLegacyAndAddOutcomeUnknown(t *testing.T) {
	t.Parallel()

	classifications := map[retry.Classification]string{
		0: "none", retry.ClassificationPermanent: "permanent",
		retry.ClassificationRetryable: "retryable", 99: "unknown",
	}
	for classification, want := range classifications {
		observation := retry.Observation{Classification: classification}
		successor := slogFields(t, observation, false)
		compatibility := slogFields(t, observation, true)
		if successor["classification"] != want || !reflect.DeepEqual(successor, compatibility) {
			t.Fatalf("classification %d successor=%v legacy=%v", classification, successor, compatibility)
		}
	}
	reasons := map[retry.Reason]string{
		"": "none", retry.ReasonSucceeded: "succeeded", retry.ReasonPermanent: "permanent",
		retry.ReasonAttemptsExhausted: "attempts_exhausted", retry.ReasonCanceled: "canceled",
		retry.ReasonElapsedBudget: "elapsed_budget", retry.ReasonSleepBudget: "sleep_budget",
		retry.ReasonAttemptBudget: "attempt_budget", retry.ReasonClassifierFailure: "classifier_failure",
		retry.ReasonSleeperFailure: "sleeper_failure", retry.ReasonWorkBudget: "work_budget", "hostile": "unknown",
	}
	for reason, want := range reasons {
		observation := retry.Observation{Reason: reason}
		successor := slogFields(t, observation, false)
		compatibility := slogFields(t, observation, true)
		if successor["reason"] != want || !reflect.DeepEqual(successor, compatibility) {
			t.Fatalf("reason %q successor=%v legacy=%v", reason, successor, compatibility)
		}
	}
	if got := slogFields(t, retry.Observation{Reason: retry.ReasonOutcomeUnknown}, false)["reason"]; got != "outcome_unknown" {
		t.Fatalf("outcome unknown reason = %v", got)
	}
}

func TestObserverSinkIsSynchronousConcurrentAndReentrant(t *testing.T) {
	t.Run("blocking and concurrent", func(t *testing.T) {
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		handler := &callbackHandler{handle: func(ctx context.Context, _ slog.Record) error {
			if ctx != context.Background() {
				t.Error("sink did not receive context.Background")
			}
			entered <- struct{}{}
			<-release
			return nil
		}}
		observer, err := retryslog.New(retryslog.Options{Logger: slog.New(handler)})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{}, 2)
		for range 2 {
			go func() { observer.Observe(retry.Observation{}); done <- struct{}{} }()
		}
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("concurrent sink did not enter")
			}
		}
		select {
		case <-done:
			close(release)
			t.Fatal("Observe returned while sink was blocked")
		default:
		}
		close(release)
		for range 2 {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Observe did not return")
			}
		}
	})

	t.Run("reentrant", func(t *testing.T) {
		var observer *retryslog.Observer
		calls := 0
		handler := &callbackHandler{handle: func(context.Context, slog.Record) error {
			calls++
			if calls == 1 {
				observer.Observe(retry.Observation{Attempt: 2})
			}
			return nil
		}}
		observer, _ = retryslog.New(retryslog.Options{Logger: slog.New(handler)})
		observer.Observe(retry.Observation{Attempt: 1})
		if calls != 2 {
			t.Fatalf("sink calls = %d", calls)
		}
	})
}

func TestDoStrictIsolatesSuccessorSlogSinkPanic(t *testing.T) {
	t.Parallel()

	observer, err := retryslog.New(retryslog.Options{Logger: slog.New(panicHandler{panicValue: "sink panic"})})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := retry.NewPolicyStrict(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: retry.SystemClock{},
		Sleeper: retry.SystemSleeper{}, Classifier: retry.RetryableClassifier(), Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoStrictDeliversReachedSlogObservationAfterCancellationRace(t *testing.T) {
	t.Parallel()

	calls := 0
	handler := &callbackHandler{handle: func(ctx context.Context, _ slog.Record) error {
		if ctx != context.Background() {
			t.Fatal("sink context is not context.Background")
		}
		calls++
		return nil
	}}
	observer, err := retryslog.New(retryslog.Options{Logger: slog.New(handler)})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := retry.NewPolicyStrict(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: retry.SystemClock{},
		Sleeper: retry.SystemSleeper{}, Classifier: retry.RetryableClassifier(), Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		cancel()
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestObserverPropagatesDirectSinkPanicSynchronously(t *testing.T) {
	t.Parallel()

	want := errors.New("sink panic")
	observer, err := retryslog.New(retryslog.Options{Logger: slog.New(panicHandler{panicValue: want})})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		//nolint:errorlint // Panic value identity is part of the handler contract.
		if recovered := recover(); recovered != want {
			t.Fatalf("recovered = %v, want exact panic", recovered)
		}
	}()
	observer.Observe(retry.Observation{})
}

func TestSuccessorSlogTypesOwnReflectionIdentity(t *testing.T) {
	t.Parallel()

	const want = "github.com/faustbrian/go-retry/adapters/slog"
	for _, value := range []any{retryslog.Options{}, retryslog.Observer{}} {
		if got := reflect.TypeOf(value).PkgPath(); got != want {
			t.Fatalf("%T PkgPath = %q", value, got)
		}
	}
}

type panicHandler struct{ panicValue any }

func (handler panicHandler) Enabled(context.Context, slog.Level) bool { return true }
func (handler panicHandler) Handle(context.Context, slog.Record) error {
	panic(handler.panicValue)
}
func (handler panicHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler panicHandler) WithGroup(string) slog.Handler      { return handler }

type callbackHandler struct {
	handle func(context.Context, slog.Record) error
}

func (*callbackHandler) Enabled(context.Context, slog.Level) bool { return true }
func (handler *callbackHandler) Handle(ctx context.Context, record slog.Record) error {
	return handler.handle(ctx, record)
}
func (handler *callbackHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler *callbackHandler) WithGroup(string) slog.Handler      { return handler }

func slogFields(t *testing.T, observation retry.Observation, compatibility bool) map[string]any {
	t.Helper()
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	if compatibility {
		observer, err := legacy.New(legacy.Options{Logger: logger, Level: slog.LevelWarn, PolicyID: "policy"})
		if err != nil {
			t.Fatal(err)
		}
		observer.Observe(observation)
	} else {
		observer, err := retryslog.New(retryslog.Options{Logger: logger, Level: slog.LevelWarn, PolicyID: "policy"})
		if err != nil {
			t.Fatal(err)
		}
		observer.Observe(observation)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "time")
	return fields
}
