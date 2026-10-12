// Package retryhttp classifies HTTP response failures and parses Retry-After.
// It does not decide whether an HTTP operation is safe to repeat. Start with
// NewClassifier and StatusError. Admission and parsing delegate to the canonical
// adapter, but Error retains its flat named identity and mutable status field.
// Foreign canonical errors may reach Transient rather than flat-status handling.
// Classifiers borrow callbacks; no HTTP client, body or lifecycle is owned.
//
// Deprecated: use github.com/faustbrian/go-retry/v2/adapters/http. This package
// remains supported through the documented compatibility interval.
package retryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	retry "github.com/faustbrian/go-retry/v2"
	canonical "github.com/faustbrian/go-retry/v2/adapters/http"
)

const (
	// MaxRetryStatuses bounds explicit status configuration.
	MaxRetryStatuses = canonical.MaxRetryStatuses
	// MaxRetryAfterBytes bounds retained Retry-After metadata.
	MaxRetryAfterBytes = canonical.MaxRetryAfterBytes
)

// ErrInvalidResponse identifies invalid or unbounded HTTP response metadata.
var ErrInvalidResponse = canonical.ErrInvalidResponse

// Options configures protocol classification. RetryStatuses replaces the
// conservative default set. Transient does not assert that replay is safe.
type Options struct {
	// RetryStatuses is copied. Nil selects defaults; an explicit empty slice
	// disables status retries. Entries must be unique integers from 100 through 999.
	RetryStatuses []int
	// Transient is an optional borrowed transport predicate. Calls are synchronous,
	// may overlap or reenter, and panic directly; synchronize captured state.
	Transient func(error) bool
}

// Classifier owns admitted status configuration and borrows Transient. The
// zero classifier treats failures as permanent; a nil pointer is rejected.
// Concurrent calls require synchronized captured predicate state.
type Classifier struct {
	inner     *canonical.Classifier
	transient func(error) bool
}

// NewClassifier validates and copies options through the canonical admission owner.
func NewClassifier(options Options) (*Classifier, error) {
	inner, err := canonical.New(canonical.Options{RetryStatuses: options.RetryStatuses, Transient: options.Transient})
	if err != nil {
		return nil, err
	}
	return &Classifier{inner: inner, transient: options.Transient}, nil
}

// Classify implements retry.Classifier. An unconfigured zero value is permanent.
func (classifier *Classifier) Classify(ctx context.Context, err error) (retry.Classification, error) {
	if classifier == nil {
		return 0, fmt.Errorf("%w: classifier is nil", retry.ErrInvalidPolicy)
	}
	if classifier.inner == nil {
		return retry.ClassificationPermanent, nil
	}
	var responseError *Error
	if errors.As(err, &responseError) {
		if responseError == nil {
			return retry.ClassificationPermanent, nil
		}
		response, admissionErr := canonical.NewError(responseError.StatusCode, nil, responseError.cause)
		if admissionErr != nil {
			//nolint:nilerr // Invalid mutable flat statuses classify permanent, not transport failure.
			return retry.ClassificationPermanent, nil
		}
		return classifier.inner.Classify(ctx, response)
	}
	if classifier.transient != nil && classifier.transient(err) {
		return retry.ClassificationRetryable, nil
	}
	return retry.ClassificationPermanent, nil
}

// Error keeps the flat package's named identity and mutable status field.
// Constructor metadata is bounded; cause traversal is explicitly caller-owned.
type Error struct {
	// StatusCode is mutable compatibility state. Invalid mutations classify permanent;
	// RetryDelay still reads the constructor-admitted metadata snapshot.
	StatusCode int
	cause      error
	response   *canonical.Error
}

// Error returns status text without formatting the cause; a nil receiver panics.
func (err *Error) Error() string { return fmt.Sprintf("HTTP status %d", err.StatusCode) }

// Unwrap exposes the borrowed cause. Unlike canonical Error, a nil receiver panics.
func (err *Error) Unwrap() error { return err.cause }

// RetryDelay uses the admitted header snapshot, independent of StatusCode
// mutation. Literal errors have no hint; a nil receiver panics.
func (err *Error) RetryDelay(now time.Time) (time.Duration, bool) {
	if err.response == nil {
		return 0, false
	}
	return err.response.RetryDelay(now)
}

// StatusError validates status and header bytes without truncating Retry-After.
// Only retry metadata is retained; response bodies and other headers remain caller-owned.
func StatusError(statusCode int, header http.Header, cause error) (*Error, error) {
	response, err := canonical.NewError(statusCode, header, cause)
	if err != nil {
		return nil, err
	}
	return &Error{StatusCode: statusCode, cause: cause, response: response}, nil
}

// ParseRetryAfter parses delta-seconds or an HTTP date using the canonical parser.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	return canonical.ParseRetryAfter(value, now)
}

var _ retry.Classifier = (*Classifier)(nil)
var _ retry.DelayHint = (*Error)(nil)
