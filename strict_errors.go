package retry

import (
	"context"
	"errors"
)

// ErrOutcomeUnknown identifies an operation whose dispatched side-effect
// outcome cannot be proved. It never implies that replay is safe.
var ErrOutcomeUnknown = errors.New("retry outcome unknown")

// MaxStrictTerminalErrorBytes bounds every human-readable DoStrict error.
const MaxStrictTerminalErrorBytes = 66

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

type strictOutcomeError struct{}

func (strictOutcomeError) Error() string { return "retry outcome unknown" }
func (strictOutcomeError) Is(target error) bool {
	return target == ErrOutcomeUnknown
}

type strictSafeCause struct {
	message string
	cause   error
}

func (cause *strictSafeCause) Error() string { return cause.message }
func (cause *strictSafeCause) Unwrap() error { return cause.cause }

type strictSafeCauses struct {
	message string
	causes  []error
}

func (causes *strictSafeCauses) Error() string   { return causes.message }
func (causes *strictSafeCauses) Unwrap() []error { return append([]error(nil), causes.causes...) }

func strictSingleCause(message string, cause error) error {
	return &strictSafeCause{message: message, cause: cause}
}

func strictMultipleCauses(message string, causes ...error) error {
	return &strictSafeCauses{message: message, causes: append([]error(nil), causes...)}
}

func strictContextError(kind error, outcome Outcome, result Result) *ContextError {
	return &ContextError{kind: kind, outcome: outcome, result: result.clone()}
}
