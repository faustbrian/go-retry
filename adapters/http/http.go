package retryhttp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	retry "github.com/faustbrian/go-retry"
)

const (
	// MaxRetryStatuses bounds explicit status configuration.
	MaxRetryStatuses = 900
	// MaxRetryAfterBytes bounds retained Retry-After metadata.
	MaxRetryAfterBytes = 128
)

var defaultRetryStatuses = []int{408, 425, 429, 500, 502, 503, 504}

// ErrInvalidResponse identifies invalid or unbounded HTTP response metadata.
var ErrInvalidResponse = errors.New("invalid HTTP retry response")

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

// New validates options and copies them into an immutable classifier.
func New(options Options) (*Classifier, error) {
	statuses := options.RetryStatuses
	if statuses == nil {
		statuses = defaultRetryStatuses
	}
	if len(statuses) > MaxRetryStatuses {
		return nil, fmt.Errorf("%w: retry statuses exceed %d entries", retry.ErrInvalidPolicy, MaxRetryStatuses)
	}
	for index, status := range statuses {
		if status < 100 || status > 999 {
			return nil, fmt.Errorf("%w: retry status %d at index %d is outside 100 through 999", retry.ErrInvalidPolicy, status, index)
		}
	}
	copied := make(map[int]struct{}, len(statuses))
	for index, status := range statuses {
		if _, exists := copied[status]; exists {
			return nil, fmt.Errorf("%w: retry status %d at index %d is duplicated", retry.ErrInvalidPolicy, status, index)
		}
		copied[status] = struct{}{}
	}
	return &Classifier{statuses: copied, transient: options.Transient}, nil
}

// Classify implements retry.Classifier. Classification does not make replay
// safe; the operation owner retains that decision.
func (classifier *Classifier) Classify(_ context.Context, err error) (retry.Classification, error) {
	if classifier == nil || classifier.statuses == nil {
		return 0, fmt.Errorf("%w: classifier is nil or uninitialized", retry.ErrInvalidPolicy)
	}
	var responseError *Error
	if errors.As(err, &responseError) {
		if _, ok := classifier.statuses[responseError.StatusCode()]; ok {
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
	statusCode int
	retryAfter string
	cause      error
}

// NewError validates and snapshots only the HTTP metadata needed for retry
// classification and delay selection.
func NewError(statusCode int, header http.Header, cause error) (*Error, error) {
	if statusCode < 100 || statusCode > 999 {
		return nil, fmt.Errorf("%w: status must be between 100 and 999", ErrInvalidResponse)
	}
	retryAfter := header.Get("Retry-After")
	if len(retryAfter) > MaxRetryAfterBytes {
		return nil, fmt.Errorf("%w: Retry-After exceeds %d bytes", ErrInvalidResponse, MaxRetryAfterBytes)
	}
	return &Error{statusCode: statusCode, retryAfter: retryAfter, cause: cause}, nil
}

// Error returns bounded response status text without formatting the cause.
func (err *Error) Error() string {
	if err == nil || err.statusCode == 0 {
		return "HTTP retry response error"
	}
	return fmt.Sprintf("HTTP status %d", err.statusCode)
}

// Unwrap returns the exact caller-provided cause.
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// StatusCode returns the validated response status.
func (err *Error) StatusCode() int {
	if err == nil {
		return 0
	}
	return err.statusCode
}

// RetryDelay implements retry.DelayHint.
func (err *Error) RetryDelay(now time.Time) (time.Duration, bool) {
	if err == nil || err.statusCode == 0 {
		return 0, false
	}
	return ParseRetryAfter(err.retryAfter, now)
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
	return max(date.Sub(now), 0), true
}

func parseSeconds(value string) (time.Duration, bool) {
	if strings.Trim(value, "0123456789") != "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Duration(math.MaxInt64), true
	}
	maximumSeconds := int64(math.MaxInt64 / int64(time.Second))
	if seconds > maximumSeconds {
		return time.Duration(math.MaxInt64), true
	}
	return time.Duration(seconds) * time.Second, true
}

var _ retry.Classifier = (*Classifier)(nil)
var _ retry.DelayHint = (*Error)(nil)
