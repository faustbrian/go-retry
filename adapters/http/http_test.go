package retryhttp_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
	retryhttp "github.com/faustbrian/go-retry/adapters/http"

	//lint:ignore SA1019 Legacy parity is the compatibility contract under test.
	legacy "github.com/faustbrian/go-retry/retryhttp" //nolint:staticcheck // Legacy parity is the compatibility contract under test.
)

func TestNewValidatesRetryStatusesDeterministically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []int
		want     string
	}{
		{"below range", []int{99}, "invalid retry policy: retry status 99 at index 0 is outside 100 through 999"},
		{"above range", []int{1000}, "invalid retry policy: retry status 1000 at index 0 is outside 100 through 999"},
		{"duplicate", []int{500, 500}, "invalid retry policy: retry status 500 at index 1 is duplicated"},
		{"range before duplicate", []int{500, 500, 99}, "invalid retry policy: retry status 99 at index 2 is outside 100 through 999"},
		{"too many", make([]int, retryhttp.MaxRetryStatuses+1), "invalid retry policy: retry statuses exceed 900 entries"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classifier, err := retryhttp.New(retryhttp.Options{RetryStatuses: test.statuses})
			if classifier != nil || err == nil || err.Error() != test.want || !errors.Is(err, retry.ErrInvalidPolicy) {
				t.Fatalf("New = (%v, %v), want nil and %q", classifier, err, test.want)
			}
		})
	}

	allStatuses := make([]int, retryhttp.MaxRetryStatuses)
	for index := range allStatuses {
		allStatuses[index] = index + 100
	}
	classifier, err := retryhttp.New(retryhttp.Options{RetryStatuses: allStatuses})
	if classifier == nil || err != nil {
		t.Fatalf("New accepted-boundary statuses = (%v, %v)", classifier, err)
	}
}

func TestNewDistinguishesDefaultsFromExplicitEmptyAndCopiesInput(t *testing.T) {
	t.Parallel()

	defaults, err := retryhttp.New(retryhttp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantDefaults := map[int]bool{408: true, 425: true, 429: true, 500: true, 502: true, 503: true, 504: true}
	for status := 100; status <= 999; status++ {
		want := retry.ClassificationPermanent
		if wantDefaults[status] {
			want = retry.ClassificationRetryable
		}
		assertHTTPClassification(t, defaults, retryhttpError(t, status, nil, nil), want)
	}

	empty, err := retryhttp.New(retryhttp.Options{RetryStatuses: []int{}})
	if err != nil {
		t.Fatal(err)
	}
	for status := 100; status <= 999; status++ {
		assertHTTPClassification(t, empty, retryhttpError(t, status, nil, nil), retry.ClassificationPermanent)
	}

	statuses := []int{503}
	copied, err := retryhttp.New(retryhttp.Options{RetryStatuses: statuses})
	if err != nil {
		t.Fatal(err)
	}
	statuses[0] = 400
	assertHTTPClassification(t, copied, retryhttpError(t, 503, nil, nil), retry.ClassificationRetryable)
	assertHTTPClassification(t, copied, retryhttpError(t, 400, nil, nil), retry.ClassificationPermanent)
}

func TestNewErrorValidatesAndBoundsResponseMetadata(t *testing.T) {
	t.Parallel()

	for _, status := range []int{99, 1000} {
		errValue, err := retryhttp.NewError(status, http.Header{"Retry-After": {strings.Repeat("x", retryhttp.MaxRetryAfterBytes+1)}}, nil)
		if errValue != nil || err == nil || err.Error() != "invalid HTTP retry response: status must be between 100 and 999" || !errors.Is(err, retryhttp.ErrInvalidResponse) {
			t.Fatalf("NewError(%d) = (%v, %v)", status, errValue, err)
		}
	}
	header := http.Header{"Retry-After": {strings.Repeat("1", retryhttp.MaxRetryAfterBytes+1)}}
	if value, err := retryhttp.NewError(503, header, nil); value != nil || err == nil || err.Error() != "invalid HTTP retry response: Retry-After exceeds 128 bytes" || !errors.Is(err, retryhttp.ErrInvalidResponse) {
		t.Fatalf("oversized NewError = (%v, %v)", value, err)
	}
	acceptedHeader := http.Header{"Retry-After": {strings.Repeat("1", retryhttp.MaxRetryAfterBytes)}}
	if value, err := retryhttp.NewError(503, acceptedHeader, nil); value == nil || err != nil {
		t.Fatalf("maximum Retry-After = (%v,%v)", value, err)
	}
	multibyteHeader := http.Header{"Retry-After": {strings.Repeat("é", retryhttp.MaxRetryAfterBytes/2+1)}}
	if value, err := retryhttp.NewError(503, multibyteHeader, nil); value != nil || !errors.Is(err, retryhttp.ErrInvalidResponse) {
		t.Fatalf("multibyte oversized NewError = (%v,%v)", value, err)
	}

	cause := &structuredCause{code: 42}
	header = http.Header{"Retry-After": {"120", "999"}, "Authorization": {"secret"}}
	value, err := retryhttp.NewError(503, header, cause)
	if err != nil {
		t.Fatal(err)
	}
	header.Set("Retry-After", "1")
	//nolint:errorlint // The response error must unwrap to the exact supplied cause.
	if value.Error() != "HTTP status 503" || value.StatusCode() != 503 || value.Unwrap() != cause || !errors.Is(value, cause) {
		t.Fatalf("value=%v status=%d unwrap=%v", value, value.StatusCode(), value.Unwrap())
	}
	var typed *structuredCause
	if !errors.As(value, &typed) || typed != cause || strings.Contains(value.Error(), "secret") || strings.Contains(value.Error(), cause.Error()) {
		t.Fatalf("cause traversal or disclosure failed: %v", value)
	}
	if delay, ok := value.RetryDelay(time.Time{}); !ok || delay != 120*time.Second {
		t.Fatalf("RetryDelay = (%s, %v)", delay, ok)
	}
}

func TestErrorNeverFormatsHostileCause(t *testing.T) {
	t.Parallel()

	cause := &hostileHTTPError{}
	value := retryhttpError(t, 503, http.Header{"Authorization": {"secret"}}, cause)
	//nolint:errorlint // The response error must retain the exact hostile cause without formatting it.
	if value.Error() != "HTTP status 503" || value.Unwrap() != cause || !errors.Is(value, cause) || cause.calls != 0 {
		t.Fatalf("safe error contract failed; cause calls=%d", cause.calls)
	}
}

func TestNewErrorAcceptsNilHeaderAndCauseWithoutRetryHint(t *testing.T) {
	t.Parallel()

	value, err := retryhttp.NewError(204, nil, nil)
	if err != nil || value.Unwrap() != nil {
		t.Fatalf("NewError = (%v,%v)", value, err)
	}
	if delay, ok := value.RetryDelay(time.Now()); delay != 0 || ok {
		t.Fatalf("RetryDelay = (%s,%v)", delay, ok)
	}
}

func TestErrorAndClassifierZeroValuesAreTotal(t *testing.T) {
	t.Parallel()

	for _, classifier := range []*retryhttp.Classifier{{}, nil} {
		classification, err := classifier.Classify(context.Background(), errors.New("ignored"))
		if classification != 0 || err == nil || err.Error() != "invalid retry policy: classifier is nil or uninitialized" || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("Classify = (%v, %v)", classification, err)
		}
	}
	for _, value := range []*retryhttp.Error{{}, nil} {
		if value.Error() != "HTTP retry response error" || value.Unwrap() != nil || value.StatusCode() != 0 {
			t.Fatalf("zero Error = (%q, %v, %d)", value.Error(), value.Unwrap(), value.StatusCode())
		}
		if delay, ok := value.RetryDelay(time.Now()); delay != 0 || ok {
			t.Fatalf("RetryDelay = (%s, %v)", delay, ok)
		}
	}
}

func TestClassifierRecognizesSuccessorBeforeTransientCallback(t *testing.T) {
	t.Parallel()

	calls := 0
	want := errors.New("transport")
	classifier, err := retryhttp.New(retryhttp.Options{
		RetryStatuses: []int{503},
		Transient: func(err error) bool {
			calls++
			//nolint:errorlint // The callback must receive the exact borrowed error.
			if err != want {
				t.Fatalf("callback error identity = %v", err)
			}
			return true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertHTTPClassification(t, classifier, retryhttpError(t, 503, nil, nil), retry.ClassificationRetryable)
	assertHTTPClassification(t, classifier, retryhttpError(t, 400, nil, nil), retry.ClassificationPermanent)
	if calls != 0 {
		t.Fatalf("successor errors reached callback %d times", calls)
	}
	response := retryhttpError(t, 503, nil, nil)
	wrapped := fmt.Errorf("outer: %w", response)
	var extracted *retryhttp.Error
	if !errors.As(wrapped, &extracted) || extracted != response {
		t.Fatalf("errors.As = %p, want %p", extracted, response)
	}
	assertHTTPClassification(t, classifier, wrapped, retry.ClassificationRetryable)
	if calls != 0 {
		t.Fatalf("wrapped successor reached callback %d times", calls)
	}
	assertHTTPClassification(t, classifier, want, retry.ClassificationRetryable)
	if calls != 1 {
		t.Fatalf("callback calls = %d", calls)
	}
	classifier, err = retryhttp.New(retryhttp.Options{Transient: func(error) bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	assertHTTPClassification(t, classifier, want, retry.ClassificationPermanent)
	classifier, err = retryhttp.New(retryhttp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertHTTPClassification(t, classifier, want, retry.ClassificationPermanent)
}

func TestParseRetryAfterMatchesLegacyAndSaturates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	values := []string{"120", " 5 ", "Sun, 19 Jul 2026 12:02:00 GMT", "Sun, 19 Jul 2026 11:59:00 GMT", "", "-1", "1.5", strings.Repeat("9", 100)}
	for _, value := range values {
		got, ok := retryhttp.ParseRetryAfter(value, now)
		legacyGot, legacyOK := legacy.ParseRetryAfter(value, now)
		if got != legacyGot || ok != legacyOK || (ok && got < 0) {
			t.Fatalf("ParseRetryAfter(%q) = (%s,%v), legacy=(%s,%v)", value, got, ok, legacyGot, legacyOK)
		}
	}
	if got, ok := retryhttp.ParseRetryAfter(strings.Repeat("9", 100), now); !ok || got != time.Duration(math.MaxInt64) {
		t.Fatalf("overflow = (%s, %v)", got, ok)
	}
	if got, ok := retryhttp.ParseRetryAfter("9223372036", now); !ok || got != 9223372036*time.Second {
		t.Fatalf("largest whole seconds = (%s, %v)", got, ok)
	}
	if got, ok := retryhttp.ParseRetryAfter("9223372037", now); !ok || got != time.Duration(math.MaxInt64) {
		t.Fatalf("whole-seconds overflow = (%s, %v)", got, ok)
	}
}

func TestSuccessorTypesOwnTheirReflectionIdentity(t *testing.T) {
	t.Parallel()

	const want = "github.com/faustbrian/go-retry/adapters/http"
	for _, value := range []any{retryhttp.Options{}, retryhttp.Classifier{}, retryhttp.Error{}} {
		if got := reflect.TypeOf(value).PkgPath(); got != want {
			t.Fatalf("%T PkgPath = %q", value, got)
		}
	}
}

func TestTransientCallbackIsSynchronousConcurrentReentrantAndPanicTransparent(t *testing.T) {
	t.Run("blocking and concurrent", func(t *testing.T) {
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		classifier, err := retryhttp.New(retryhttp.Options{Transient: func(error) bool {
			entered <- struct{}{}
			<-release
			return false
		}})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{}, 2)
		var releaseOnce sync.Once
		for range 2 {
			go func() {
				_, _ = classifier.Classify(context.Background(), errors.New("borrowed"))
				done <- struct{}{}
			}()
		}
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				releaseOnce.Do(func() { close(release) })
				for range 2 {
					select {
					case <-done:
					case <-time.After(time.Second):
					}
				}
				t.Fatal("concurrent callback did not enter")
			}
		}
		select {
		case <-done:
			t.Fatal("Classify returned while callback was blocked")
		default:
		}
		releaseOnce.Do(func() { close(release) })
		for range 2 {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("concurrent Classify did not return")
			}
		}
	})

	t.Run("reentrant", func(t *testing.T) {
		outer := errors.New("outer")
		inner := errors.New("inner")
		var classifier *retryhttp.Classifier
		classifier, _ = retryhttp.New(retryhttp.Options{Transient: func(err error) bool {
			//nolint:errorlint // The callback must receive the exact borrowed error.
			if err == outer {
				classification, classifyErr := classifier.Classify(context.Background(), inner)
				if classifyErr != nil || classification != retry.ClassificationPermanent {
					t.Fatalf("inner = (%v,%v)", classification, classifyErr)
				}
				return true
			}
			return false
		}})
		assertHTTPClassification(t, classifier, outer, retry.ClassificationRetryable)
	})

	t.Run("cancellation does not preempt callback", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		entered := make(chan struct{})
		release := make(chan struct{})
		borrowed := errors.New("borrowed")
		classifier, err := retryhttp.New(retryhttp.Options{Transient: func(got error) bool {
			//nolint:errorlint // The callback must receive the exact borrowed error.
			if got != borrowed {
				t.Fatal("callback error identity changed")
			}
			close(entered)
			<-release
			return true
		}})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			classification, classifyErr := classifier.Classify(ctx, borrowed)
			if classifyErr != nil || classification != retry.ClassificationRetryable {
				t.Errorf("Classify = (%v,%v)", classification, classifyErr)
			}
			close(done)
		}()
		<-entered
		cancel()
		select {
		case <-done:
			close(release)
			t.Fatal("cancellation preempted callback")
		default:
		}
		close(release)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Classify did not return")
		}
	})

	t.Run("panic", func(t *testing.T) {
		want := errors.New("callback panic")
		classifier, _ := retryhttp.New(retryhttp.Options{Transient: func(error) bool { panic(want) }})
		defer func() {
			//nolint:errorlint // Panic value identity is part of the callback contract.
			if recovered := recover(); recovered != want {
				t.Fatalf("recovered = %v", recovered)
			}
		}()
		_, _ = classifier.Classify(context.Background(), errors.New("borrowed"))
	})
}

func retryhttpError(t *testing.T, status int, header http.Header, cause error) *retryhttp.Error {
	t.Helper()
	value, err := retryhttp.NewError(status, header, cause)
	if err != nil {
		t.Fatalf("NewError: %v", err)
	}
	return value
}

func assertHTTPClassification(t *testing.T, classifier *retryhttp.Classifier, err error, want retry.Classification) {
	t.Helper()
	got, classifyErr := classifier.Classify(context.Background(), err)
	if classifyErr != nil || got != want {
		t.Fatalf("Classify(%v) = (%v, %v), want %v", err, got, classifyErr, want)
	}
}

type structuredCause struct{ code int }

func (cause *structuredCause) Error() string { return "sensitive structured cause" }

type hostileHTTPError struct{ calls int }

func (err *hostileHTTPError) Error() string {
	err.calls++
	panic("hostile HTTP cause formatted")
}
