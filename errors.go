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
// outcome precedence belong to the executor, not this classifier.
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

// BudgetKind identifies the configured budget that stopped execution.
type BudgetKind string

const (
	// BudgetElapsed identifies the total elapsed-time budget.
	BudgetElapsed BudgetKind = "elapsed"
	// BudgetSleep identifies the accumulated-sleep budget.
	BudgetSleep BudgetKind = "sleep"
	// BudgetDeadline identifies a retry delay that cannot finish before the
	// caller context deadline.
	BudgetDeadline BudgetKind = "deadline"
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
	case BudgetElapsed, BudgetSleep, BudgetDeadline, BudgetAttempt, BudgetWork:
		return fmt.Sprintf("retry %s budget exhausted", err.Kind)
	default:
		return "retry budget exhausted"
	}
}

// Unwrap exposes the retained machine cause, which may contain private data.
func (err *BudgetError) Unwrap() error { return err.cause }

// Result returns a defensive copy of terminal metadata.
func (err *BudgetError) Result() Result { return err.result.clone() }

// ErrOutcomeUnknown identifies an operation whose dispatched side-effect
// outcome cannot be proved. It never implies that replay is safe.
var ErrOutcomeUnknown = errors.New("retry outcome unknown")

// MaxTerminalErrorBytes bounds human-readable errors returned by Do,
// not panic payloads, retained causes or value formatting.
const MaxTerminalErrorBytes = 66

// Outcome describes whether an operation was dispatched and whether its
// side-effect result is known.
type Outcome uint8

const (
	// OutcomeNotDispatched means the operation callback was not invoked.
	OutcomeNotDispatched Outcome = iota
	// OutcomeKnown means the callback conclusively described its result.
	OutcomeKnown
	// OutcomeUnknown means dispatch occurred but the result cannot be proved.
	OutcomeUnknown
)

// ContextError reports a normalized cancellation or deadline boundary without
// retaining an arbitrary cancellation, sleeper, or operation cause.
type ContextError struct {
	kind    error
	outcome Outcome
	result  Result
}

// Error returns bounded, stable context or unknown-outcome text.
func (err *ContextError) Error() string {
	if err == nil || err.kind == nil {
		return "retry context error"
	}
	if err.outcome == OutcomeUnknown {
		return "retry outcome unknown"
	}
	if errors.Is(err.kind, context.DeadlineExceeded) {
		return "retry deadline exceeded"
	}
	return "retry canceled"
}

// Unwrap returns only a normalized context sentinel.
func (err *ContextError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.kind
}

// Result returns a defensive copy of terminal retry metadata.
func (err *ContextError) Result() Result {
	if err == nil {
		return Result{}
	}
	return err.result.clone()
}

// Outcome returns the machine-readable dispatch outcome.
func (err *ContextError) Outcome() Outcome {
	if err == nil {
		return OutcomeNotDispatched
	}
	return err.outcome
}

// Is matches ErrOutcomeUnknown only for unknown outcomes. Context sentinels
// remain available through Unwrap.
func (err *ContextError) Is(target error) bool {
	return err != nil && err.outcome == OutcomeUnknown && target == ErrOutcomeUnknown
}

type outcomeError struct{}

func (outcomeError) Error() string { return "retry outcome unknown" }
func (outcomeError) Is(target error) bool {
	return target == ErrOutcomeUnknown
}

// These carriers hide human-readable cause text while preserving exact machine
// identities for errors.Is/As. They do not erase or sanitize the cause object.
type safeCause struct {
	message string
	cause   error
}

func (cause *safeCause) Error() string { return cause.message }
func (cause *safeCause) Unwrap() error { return cause.cause }

type safeCauses struct {
	message string
	causes  []error
}

func (causes *safeCauses) Error() string   { return causes.message }
func (causes *safeCauses) Unwrap() []error { return append([]error(nil), causes.causes...) }

func singleCause(message string, cause error) error {
	return &safeCause{message: message, cause: cause}
}

func multipleCauses(message string, causes ...error) error {
	return &safeCauses{message: message, causes: append([]error(nil), causes...)}
}

func contextError(kind error, outcome Outcome, result Result) *ContextError {
	return &ContextError{kind: kind, outcome: outcome, result: result.clone()}
}
