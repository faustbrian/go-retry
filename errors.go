package retry

import (
	"context"
	"errors"
	"fmt"
)

// RetryableError explicitly marks a cause as eligible for bounded retry.
type RetryableError struct {
	// Cause is borrowed for errors.Is/As traversal, never formatted by Error.
	Cause error
}

// Error returns a fixed bounded category, not the retained cause text.
func (err *RetryableError) Error() string { return "retryable error" }

// Unwrap exposes the retained machine cause, which may contain private data.
func (err *RetryableError) Unwrap() error { return err.Cause }

// Retryable marks err as eligible for an explicitly configured retry policy.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Cause: err}
}

// PermanentError explicitly marks a cause as ineligible for retry.
type PermanentError struct {
	// Cause is borrowed for errors.Is/As traversal, never formatted by Error.
	Cause error
}

// Error returns a fixed bounded category, not the retained cause text.
func (err *PermanentError) Error() string { return "permanent error" }

// Unwrap exposes the retained machine cause, which may contain private data.
func (err *PermanentError) Unwrap() error { return err.Cause }

// Permanent marks err as ineligible for retry.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Cause: err}
}

// RetryableClassifier returns a classifier that retries RetryableError values
// and treats every other error as permanent. It does not inspect PermanentError
// first: a nested RetryableError still makes the chain retryable. Context and
// outcome precedence belong to the execution variant, not this classifier.
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

// Error returns a fixed bounded category, not the retained cause text.
func (err *ExhaustedError) Error() string {
	return "retry attempts exhausted"
}

// Unwrap exposes the retained machine cause, which may contain private data.
func (err *ExhaustedError) Unwrap() error { return err.cause }

// Result returns a defensive copy of terminal metadata.
func (err *ExhaustedError) Result() Result { return err.result.clone() }

// CanceledError reports cancellation by the caller or its deadline.
type CanceledError struct {
	cause  error
	result Result
}

// Error returns a fixed bounded category, not the retained cause text.
func (err *CanceledError) Error() string { return "retry canceled" }

// Unwrap exposes the retained machine cause, which may contain private data.
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
	// BudgetWork identifies denial by the shared retry-plus-hedge work budget.
	BudgetWork BudgetKind = "work"
)

// BudgetError reports exhaustion of an elapsed, sleep, attempt, or shared-work budget.
type BudgetError struct {
	// Kind selects a known budget category; unrecognized values use generic text.
	Kind   BudgetKind
	cause  error
	result Result
}

// Error returns a fixed bounded category, not the retained cause text.
func (err *BudgetError) Error() string {
	switch err.Kind {
	case BudgetElapsed, BudgetSleep, BudgetAttempt, BudgetWork:
		return fmt.Sprintf("retry %s budget exhausted", err.Kind)
	default:
		return "retry budget exhausted"
	}
}

// Unwrap exposes the retained machine cause, which may contain private data.
func (err *BudgetError) Unwrap() error { return err.cause }

// Result returns a defensive copy of terminal metadata.
func (err *BudgetError) Result() Result { return err.result.clone() }
