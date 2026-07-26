package retrylog_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/faustbrian/golib/pkg/retry/retrylog"
)

func TestObserverLogsOnlyBoundedRetryFields(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	observer, err := retrylog.New(retrylog.Options{
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
		Level:  slog.LevelInfo, PolicyID: "invoice-read",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	observer.Observe(retry.Observation{
		Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second,
		Classification: retry.ClassificationRetryable, Reason: retry.ReasonSleepBudget,
	})
	log := output.String()
	for _, field := range []string{"invoice-read", `"attempt":2`, `"elapsed_ns":3000000000`, `"next_delay_ns":1000000000`, "sleep_budget"} {
		if !strings.Contains(log, field) {
			t.Errorf("log %q does not contain %q", log, field)
		}
	}
	if strings.Contains(log, "error") {
		t.Fatalf("bounded log unexpectedly contains an error payload: %q", log)
	}
}

func TestObserverRejectsMissingLoggerAndUnboundedPolicyID(t *testing.T) {
	t.Parallel()

	if _, err := retrylog.New(retrylog.Options{}); !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("missing logger error = %v", err)
	}
	if _, err := retrylog.New(retrylog.Options{Logger: slog.Default(), PolicyID: strings.Repeat("x", retrylog.MaxPolicyIDLength+1)}); !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("long policy ID error = %v", err)
	}
}

func TestObserverBoundsUnknownEnums(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	observer, err := retrylog.New(retrylog.Options{Logger: slog.New(slog.NewJSONHandler(&output, nil))})
	if err != nil {
		t.Fatal(err)
	}
	classifications := []retry.Classification{0, retry.ClassificationPermanent, retry.ClassificationRetryable, 99}
	reasons := []retry.Reason{"", retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted,
		retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget, retry.ReasonAttemptBudget,
		retry.ReasonClassifierFailure, retry.ReasonSleeperFailure, "hostile"}
	for _, classification := range classifications {
		observer.Observe(retry.Observation{Classification: classification})
	}
	for _, reason := range reasons {
		observer.Observe(retry.Observation{Reason: reason})
	}
	if !strings.Contains(output.String(), "unknown") {
		t.Fatalf("output does not bound unknown values: %s", output.String())
	}
}
