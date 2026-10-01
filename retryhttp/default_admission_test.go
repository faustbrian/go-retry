package retryhttp_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v2"
	canonical "github.com/faustbrian/go-retry/v2/adapters/http"
	"github.com/faustbrian/go-retry/v2/retryhttp"
)

func TestFlatHTTPRejectsUnboundedConstructorInputs(t *testing.T) {
	t.Run("statuses", func(t *testing.T) {
		statuses := make([]int, retryhttp.MaxRetryStatuses+1)
		for index := range statuses {
			statuses[index] = 500
		}
		if classifier, err := retryhttp.NewClassifier(retryhttp.Options{RetryStatuses: statuses}); classifier != nil || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatal("over-limit status configuration produced a classifier")
		}
	})
	t.Run("header", func(t *testing.T) {
		value, err := retryhttp.StatusError(503, http.Header{"Retry-After": {strings.Repeat("1", retryhttp.MaxRetryAfterBytes+1)}}, nil)
		if value != nil || !errors.Is(err, retryhttp.ErrInvalidResponse) {
			t.Fatal("over-limit header produced a response error instead of admission rejection")
		}
	})
}

func TestFlatHTTPDefaultErrorDoesNotFormatPrivateCause(t *testing.T) {
	cause := &privateHTTPCause{}
	err, admissionErr := retryhttp.StatusError(503, nil, cause)
	if admissionErr != nil {
		t.Fatal(admissionErr)
	}
	text := err.Error()
	var recovered *privateHTTPCause
	if text != "HTTP status 503" || cause.calls != 0 || !errors.Is(err, cause) || !errors.As(err, &recovered) || recovered != cause {
		t.Fatalf("text=%q cause calls=%d preserved=%t", text, cause.calls, recovered == cause)
	}
}

func TestFlatHTTPAdmissionLimitsAndNamedCompatibility(t *testing.T) {
	for _, statuses := range [][]int{{99}, {1000}, {500, 500}} {
		if value, err := retryhttp.NewClassifier(retryhttp.Options{RetryStatuses: statuses}); value != nil || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatal("invalid status configuration accepted")
		}
	}
	statuses := make([]int, retryhttp.MaxRetryStatuses)
	for index := range statuses {
		statuses[index] = 100 + index
	}
	all := mustFlatClassifier(t, retryhttp.Options{RetryStatuses: statuses})
	statuses[0] = 999
	for _, status := range []int{100, 999} {
		classification, err := all.Classify(context.Background(), &retryhttp.Error{StatusCode: status})
		if err != nil || classification != retry.ClassificationRetryable {
			t.Fatal("exact status boundary or snapshot changed")
		}
	}
	for _, status := range []int{99, 1000} {
		if value, err := retryhttp.StatusError(status, nil, nil); value != nil || !errors.Is(err, retryhttp.ErrInvalidResponse) {
			t.Fatal("invalid response status accepted")
		}
	}
	header := http.Header{"Retry-After": {strings.Repeat("0", retryhttp.MaxRetryAfterBytes-1) + "1"}}
	response := mustFlatError(t, 503, header, nil)
	header.Set("Retry-After", "9")
	if delay, ok := response.RetryDelay(time.Time{}); !ok || delay != time.Second {
		t.Fatal("exact header boundary or snapshot changed")
	}
	for _, test := range []struct {
		statuses []int
		want     retry.Classification
	}{{nil, retry.ClassificationRetryable}, {[]int{}, retry.ClassificationPermanent}} {
		classifier := mustFlatClassifier(t, retryhttp.Options{RetryStatuses: test.statuses})
		classification, err := classifier.Classify(context.Background(), response)
		if err != nil || classification != test.want {
			t.Fatal("default/empty selection changed")
		}
	}
	transientCalls := 0
	classifier := mustFlatClassifier(t, retryhttp.Options{Transient: func(error) bool { transientCalls++; return true }})
	response.StatusCode = 99
	classification, err := classifier.Classify(context.Background(), response)
	var flat *retryhttp.Error
	if err != nil || classification != retry.ClassificationPermanent || transientCalls != 0 || !errors.As(response, &flat) || flat != response {
		t.Fatal("flat field/type compatibility changed")
	}
}

type privateHTTPCause struct{ calls int }

func TestFlatHTTPKeepsForeignErrorAndContextClassificationVariants(t *testing.T) {
	foreign, err := canonical.NewError(503, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	classifier := mustFlatClassifier(t, retryhttp.Options{Transient: func(received error) bool {
		calls++
		//nolint:errorlint // The predicate must receive the exact foreign error, not a wrapper.
		if received != foreign {
			t.Fatal("transient received a different error")
		}
		return false
	}})
	classification, err := classifier.Classify(context.Background(), foreign)
	if err != nil || classification != retry.ClassificationPermanent || calls != 1 {
		t.Fatalf("foreign classification=%v transientcalls=%d", classification, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	classification, err = classifier.Classify(ctx, mustFlatError(t, 503, nil, nil))
	if err != nil || classification != retry.ClassificationRetryable || calls != 1 {
		t.Fatal("flat context/status precedence changed")
	}
}

func (cause *privateHTTPCause) Error() string {
	cause.calls++
	return "application-private"
}
