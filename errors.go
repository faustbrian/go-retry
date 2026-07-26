package retry

import (
	"context"
	"errors"
	"fmt"
)

// RetryableError explicitly marks a cause as eligible for bounded retry.
type RetryableError struct{ Cause error }

func (err *RetryableError) Error() string { return fmt.Sprintf("retryable: %v", err.Cause) }
func (err *RetryableError) Unwrap() error { return err.Cause }

// Retryable marks err as eligible for an explicitly configured retry policy.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Cause: err}
}

// PermanentError explicitly marks a cause as ineligible for retry.
type PermanentError struct{ Cause error }

func (err *PermanentError) Error() string { return fmt.Sprintf("permanent: %v", err.Cause) }
func (err *PermanentError) Unwrap() error { return err.Cause }

// Permanent marks err as ineligible for retry.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Cause: err}
}

// RetryableClassifier returns a classifier that retries RetryableError values
// and treats every other error as permanent.
func RetryableClassifier() Classifier {
	return ClassifyFunc(func(_ context.Context, err error) (Classification, error) {
		var retryable *RetryableError
		if errors.As(err, &retryable) {
			return ClassificationRetryable, nil
		}
		return ClassificationPermanent, nil
	})
}

// ExhaustedError reports that MaxAttempts stopped a retryable operation.
type ExhaustedError struct {
	cause  error
	result Result
}

func (err *ExhaustedError) Error() string {
	return fmt.Sprintf("retry attempts exhausted: %v", err.cause)
}
func (err *ExhaustedError) Unwrap() error { return err.cause }

// Result returns a defensive copy of terminal metadata.
func (err *ExhaustedError) Result() Result { return err.result.clone() }

// CanceledError reports cancellation by the caller or its deadline.
type CanceledError struct {
	cause  error
	result Result
}

func (err *CanceledError) Error() string { return fmt.Sprintf("retry canceled: %v", err.cause) }
func (err *CanceledError) Unwrap() error { return err.cause }

// Result returns a defensive copy of terminal metadata.
func (err *CanceledError) Result() Result { return err.result.clone() }

// BudgetKind identifies the configured budget that stopped execution.
type BudgetKind string

const (
	// BudgetElapsed identifies the total elapsed-time budget.
	BudgetElapsed BudgetKind = "elapsed"
	// BudgetSleep identifies the accumulated-sleep budget.
	BudgetSleep BudgetKind = "sleep"
	// BudgetAttempt identifies a per-attempt timeout.
	BudgetAttempt BudgetKind = "attempt"
)

// BudgetError reports exhaustion of an elapsed, sleep, or attempt budget.
type BudgetError struct {
	Kind   BudgetKind
	cause  error
	result Result
}

func (err *BudgetError) Error() string {
	return fmt.Sprintf("retry %s budget exhausted: %v", err.Kind, err.cause)
}
func (err *BudgetError) Unwrap() error { return err.cause }

// Result returns a defensive copy of terminal metadata.
func (err *BudgetError) Result() Result { return err.result.clone() }
