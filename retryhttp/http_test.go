package retryhttp_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/faustbrian/golib/pkg/retry/retryhttp"
)

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"120", 2 * time.Minute, true},
		{"Sun, 19 Jul 2026 12:02:00 GMT", 2 * time.Minute, true},
		{"Sun, 19 Jul 2026 11:59:00 GMT", 0, true},
		{" 5 ", 5 * time.Second, true},
		{"-1", 0, false},
		{"1.5", 0, false},
		{"tomorrow", 0, false},
		{"", 0, false},
	}

	for _, test := range tests {
		delay, ok := retryhttp.ParseRetryAfter(test.value, now)
		if delay != test.want || ok != test.ok {
			t.Errorf("ParseRetryAfter(%q) = (%s, %v), want (%s, %v)", test.value, delay, ok, test.want, test.ok)
		}
	}
}

func TestParseRetryAfterSaturatesOversizedSeconds(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"9223372037", strings.Repeat("9", 100)} {
		if delay, ok := retryhttp.ParseRetryAfter(value, time.Time{}); !ok || delay != time.Duration(1<<63-1) {
			t.Fatalf("ParseRetryAfter(%q) = (%s, %v)", value, delay, ok)
		}
	}
}

func TestClassifierUsesConservativeHTTPStatusSet(t *testing.T) {
	t.Parallel()

	classifier := retryhttp.NewClassifier(retryhttp.Options{})
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		classification, err := classifier.Classify(context.Background(), retryhttp.StatusError(status, nil, nil))
		if err != nil || classification != retry.ClassificationRetryable {
			t.Errorf("status %d = (%v, %v), want retryable", status, classification, err)
		}
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusNotImplemented} {
		classification, err := classifier.Classify(context.Background(), retryhttp.StatusError(status, nil, nil))
		if err != nil || classification != retry.ClassificationPermanent {
			t.Errorf("status %d = (%v, %v), want permanent", status, classification, err)
		}
	}
}

func TestClassifierSupportsExplicitTransportPredicate(t *testing.T) {
	t.Parallel()

	want := errors.New("connection reset")
	classifier := retryhttp.NewClassifier(retryhttp.Options{
		RetryStatuses: []int{999, 42},
		Transient:     func(err error) bool { return errors.Is(err, want) },
	})
	classification, err := classifier.Classify(context.Background(), want)
	if err != nil || classification != retry.ClassificationRetryable {
		t.Fatalf("transient = (%v, %v)", classification, err)
	}
	classification, err = classifier.Classify(context.Background(), errors.New("unknown"))
	if err != nil || classification != retry.ClassificationPermanent {
		t.Fatalf("unknown = (%v, %v)", classification, err)
	}
}

func TestStatusErrorPreservesCauseAndOnlyBoundedMetadata(t *testing.T) {
	t.Parallel()

	withoutCause := retryhttp.StatusError(http.StatusServiceUnavailable, nil, nil)
	if withoutCause.Error() != "HTTP status 503" {
		t.Fatalf("error = %q", withoutCause.Error())
	}
	cause := errors.New("transport")
	withCause := retryhttp.StatusError(http.StatusBadGateway, nil, cause)
	if !errors.Is(withCause, cause) || !strings.Contains(withCause.Error(), "transport") {
		t.Fatalf("error = %v", withCause)
	}
}

func TestRetryAfterOverridesBackoffButStillHonorsMaximumDelay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now}
	sleeper := &recordingSleeper{}
	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.Constant(time.Second), MaxAttempts: 2, MaxDelay: 3 * time.Second,
		Clock: clock, Sleeper: sleeper, Classifier: retryhttp.NewClassifier(retryhttp.Options{}),
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	header := make(http.Header)
	header.Set("Retry-After", "10")
	calls := 0
	_, _, err = retry.Do(context.Background(), policy, func(context.Context) (struct{}, error) {
		calls++
		if calls == 1 {
			return struct{}{}, retryhttp.StatusError(http.StatusTooManyRequests, header, errors.New("busy"))
		}
		return struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(sleeper.delays) != 1 || sleeper.delays[0] != 3*time.Second {
		t.Fatalf("sleep delays = %v, want [3s]", sleeper.delays)
	}
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type recordingSleeper struct{ delays []time.Duration }

func (sleeper *recordingSleeper) Sleep(_ context.Context, delay time.Duration) error {
	sleeper.delays = append(sleeper.delays, delay)
	return nil
}
