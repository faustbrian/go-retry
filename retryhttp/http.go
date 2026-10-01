// Package retryhttp classifies HTTP response failures and parses Retry-After.
// It does not decide whether an HTTP operation is safe to repeat.
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
	RetryStatuses []int
	Transient     func(error) bool
}

// Classifier classifies HTTP failures without making idempotency decisions.
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
	StatusCode int
	cause      error
	response   *canonical.Error
}

// Error returns bounded status text without formatting the cause.
func (err *Error) Error() string { return fmt.Sprintf("HTTP status %d", err.StatusCode) }
func (err *Error) Unwrap() error { return err.cause }

// RetryDelay implements retry.DelayHint using the admitted header snapshot.
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
