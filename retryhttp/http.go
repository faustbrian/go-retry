// Package retryhttp classifies HTTP response failures and parses Retry-After.
// It does not decide whether an HTTP operation is safe to repeat.
package retryhttp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

var defaultRetryStatuses = []int{
	http.StatusRequestTimeout,
	http.StatusTooEarly,
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

// Options configures protocol classification. RetryStatuses replaces the
// conservative default set. Transient may classify transport errors, but does
// not assert that replay is safe.
type Options struct {
	RetryStatuses []int
	Transient     func(error) bool
}

// Classifier classifies HTTP failures without making idempotency decisions.
type Classifier struct {
	statuses  map[int]struct{}
	transient func(error) bool
}

// NewClassifier copies options into an immutable classifier.
func NewClassifier(options Options) *Classifier {
	statuses := options.RetryStatuses
	if statuses == nil {
		statuses = defaultRetryStatuses
	}
	copied := make(map[int]struct{}, len(statuses))
	for _, status := range statuses {
		if status >= 100 && status <= 999 {
			copied[status] = struct{}{}
		}
	}
	return &Classifier{statuses: copied, transient: options.Transient}
}

// Classify implements retry.Classifier.
func (classifier *Classifier) Classify(_ context.Context, err error) (retry.Classification, error) {
	var responseError *Error
	if errors.As(err, &responseError) {
		if _, ok := classifier.statuses[responseError.StatusCode]; ok {
			return retry.ClassificationRetryable, nil
		}
		return retry.ClassificationPermanent, nil
	}
	if classifier.transient != nil && classifier.transient(err) {
		return retry.ClassificationRetryable, nil
	}
	return retry.ClassificationPermanent, nil
}

// Error contains bounded response metadata and preserves an optional cause.
type Error struct {
	StatusCode int
	retryAfter string
	cause      error
}

func (err *Error) Error() string {
	if err.cause == nil {
		return fmt.Sprintf("HTTP status %d", err.StatusCode)
	}
	return fmt.Sprintf("HTTP status %d: %v", err.StatusCode, err.cause)
}

func (err *Error) Unwrap() error { return err.cause }

// RetryDelay implements retry.DelayHint.
func (err *Error) RetryDelay(now time.Time) (time.Duration, bool) {
	return ParseRetryAfter(err.retryAfter, now)
}

// StatusError constructs a bounded HTTP error. Only Retry-After is retained
// from header; response bodies and other headers remain caller-owned.
func StatusError(statusCode int, header http.Header, cause error) error {
	retryAfter := ""
	if header != nil {
		retryAfter = header.Get("Retry-After")
	}
	return &Error{StatusCode: statusCode, retryAfter: retryAfter, cause: cause}
}

// ParseRetryAfter parses delta-seconds or an HTTP date. Past dates produce an
// immediate retry hint. Oversized delta-seconds saturate safely.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, ok := parseSeconds(value); ok {
		return seconds, true
	}
	date, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := date.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func parseSeconds(value string) (time.Duration, bool) {
	seconds := uint64(0)
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
		digit := uint64(character - '0')
		if seconds > (math.MaxUint64-digit)/10 {
			return time.Duration(math.MaxInt64), true
		}
		seconds = seconds*10 + digit
	}
	maximumSeconds := uint64(math.MaxInt64 / int64(time.Second))
	if seconds > maximumSeconds {
		return time.Duration(math.MaxInt64), true
	}
	return time.Duration(seconds) * time.Second, true
}

var _ retry.Classifier = (*Classifier)(nil)
var _ retry.DelayHint = (*Error)(nil)
