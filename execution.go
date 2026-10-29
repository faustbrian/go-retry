package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/faustbrian/go-resilience/v2"
)

// Reason identifies why execution stopped.
type Reason string

const (
	// ReasonSucceeded identifies a successful operation.
	ReasonSucceeded Reason = "succeeded"
	// ReasonPermanent identifies an explicitly permanent failure.
	ReasonPermanent Reason = "permanent"
	// ReasonAttemptsExhausted identifies maximum-attempt exhaustion.
	ReasonAttemptsExhausted Reason = "attempts_exhausted"
	// ReasonCanceled identifies caller cancellation or deadline.
	ReasonCanceled Reason = "canceled"
	// ReasonElapsedBudget identifies total elapsed-time exhaustion.
	ReasonElapsedBudget Reason = "elapsed_budget"
	// ReasonSleepBudget identifies accumulated-sleep exhaustion.
	ReasonSleepBudget Reason = "sleep_budget"
	// ReasonDeadlineBudget identifies a delay that cannot finish before the
	// caller context deadline.
	ReasonDeadlineBudget Reason = "deadline_budget"
	// ReasonAttemptBudget identifies a per-attempt timeout.
	ReasonAttemptBudget Reason = "attempt_budget"
	// ReasonClassifierFailure identifies a classifier error or invalid result.
	ReasonClassifierFailure Reason = "classifier_failure"
	// ReasonSleeperFailure identifies a non-context sleeper failure.
	ReasonSleeperFailure Reason = "sleeper_failure"
	// ReasonWorkBudget identifies local denial by the shared amplification budget.
	ReasonWorkBudget Reason = "work_budget"
)

// Attempt records an unsuccessful dispatch, not a successful operation value.
// Classified failures retain the original error object; unknown/invalid
// outcomes retain no error and have zero Classification and Delay.
type Attempt struct {
	// Attempt is the one-based physical callback count.
	Attempt uint
	// Elapsed is nonnegative clock-sampled time since execution began.
	Elapsed time.Duration
	// Delay is the selected next pause, even if a budget rejects it before sleep.
	Delay time.Duration
	// Classification is the accepted result, or zero for unclassified outcomes.
	Classification Classification
	// Err retains a classified operation error, or nil for unclassified outcomes.
	// Retained errors are not copied or safe log text.
	Err error
}

// Result contains retry metadata rather than the returned T. Retained errors
// may reference arbitrary caller data; history is not a byte or privacy bound.
type Result struct {
	// Attempts counts dispatched callbacks, including success or ambiguous results.
	Attempts uint
	// Elapsed includes observed collaborator time using the injected Clock.
	Elapsed time.Duration
	// FinalDelay is the last selected delay, not proof that it was slept.
	FinalDelay time.Duration
	// Reason is the terminal category, not a replay-safety decision.
	Reason Reason
	// History is the most recent recorded failures, bounded by count.
	// Terminal metadata defensively copies the slice. Error objects remain borrowed.
	History []Attempt
}

// DelayHint is implemented by classified errors that carry a server-provided
// minimum retry delay, such as HTTP Retry-After. Hints are advisory: MinDelay
// and MaxDelay still apply. A positive hint can be shortened by MaxDelay.
type DelayHint interface {
	// RetryDelay reports an available delay at the supplied clock time. Calls
	// are synchronous without context, timeout or panic recovery.
	RetryDelay(time.Time) (time.Duration, bool)
}

func (result Result) clone() Result {
	result.History = append([]Attempt(nil), result.History...)
	return result
}

// Observer receives scalar lifecycle metadata synchronously. Calls may overlap
// or reenter execution; no package lock is held. Engine calls contain panics,
// but direct calls propagate them. Blocking observers block the retry caller.
// Notifications are not a complete audit of all attempt or terminal paths.
type Observer interface {
	// Observe consumes a copied notification with no operation value or error.
	Observe(Observation)
}

// Observation describes a reached notification site; it is not proof of a
// physical dispatch. Early work-budget refusal can report attempt zero, and
// several cancellation, budget and collaborator-failure paths emit no event.
type Observation struct {
	// Attempt is the completed callback ordinal, or zero before first dispatch.
	Attempt uint
	// Elapsed is the sampled elapsed time at this notification.
	Elapsed time.Duration
	// NextDelay is a selected pause, not a completed sleep.
	NextDelay time.Duration
	// Classification is zero when no failure was classified.
	Classification Classification
	// Reason is empty for a selected retry pause and populated for terminal events.
	Reason Reason
}

// ObserveFunc borrows a callback as Observer. A nil function panics on direct
// invocation; policy construction rejects explicitly typed-nil collaborators.
type ObserveFunc func(Observation)

// Observe invokes the adapted function.
func (function ObserveFunc) Observe(observation Observation) { function(observation) }

// ReasonOutcomeUnknown identifies a dispatched operation whose result cannot
// be proved.
const ReasonOutcomeUnknown Reason = "outcome_unknown"

// AttemptResult declares the value and side-effect outcome of one attempt.
type AttemptResult[T any] struct {
	// Value is returned to the caller only with OutcomeKnown and nil error.
	Value T
	// Outcome must be OutcomeKnown, or OutcomeUnknown with a non-nil error.
	// OutcomeNotDispatched is invalid after invocation; errors alone do not
	// establish the dispatch outcome.
	Outcome Outcome
}

// ExecutionResult contains a value only for known success, bounded retry
// metadata, and the operation dispatch outcome.
type ExecutionResult[T any] struct {
	// Value is set only for known success and otherwise is the zero T.
	Value T
	// Retry contains bounded metadata; retained error objects may be private.
	Retry Result
	// Outcome identifies dispatch knowledge, never permission to repeat side effects.
	Outcome Outcome
}

// Operation executes one attempt and declares whether its outcome is
// known. It must never label an ambiguous side effect as known or return
// OutcomeNotDispatched after invocation. Captured state is borrowed; calls
// execute synchronously and operation panics propagate, with owned cleanup.
type Operation[T any] func(context.Context) (AttemptResult[T], error)

// Do executes operation under policy without retrying an unknown outcome
// and returns only bounded human-readable terminal errors. Nil/typed-nil ctx,
// nil policy or operation returns ErrInvalidPolicy. Use a constructed policy.
// Known success wins caller/attempt deadline races. Known failures retain
// timeout and classification precedence before later caller-cancellation checks.
// Unknown or invalid outcomes
// terminate without classification, delay selection or retry.
// Human text is safe by category, not arbitrary cause/history traversal.
// Collaborators may block or overlap; deadlines are cooperative. Acquired
// permits and attempt contexts are released, not side effects rolled back.
// Permit completion errors are ignored and completion/operation panics propagate.
func Do[T any](ctx context.Context, policy *Policy, operation Operation[T]) (ExecutionResult[T], error) {
	if nilLike(ctx) || policy == nil || policy.config.MaxAttempts == 0 || operation == nil {
		return ExecutionResult[T]{}, fmt.Errorf("%w: context, policy, and operation are required", ErrInvalidPolicy)
	}

	start := policy.config.Clock.Now()
	result := Result{}
	if err := ctx.Err(); err != nil {
		result = finish(policy, start, result, ReasonCanceled)
		return ExecutionResult[T]{Retry: result, Outcome: OutcomeNotDispatched}, contextError(normalizedContext(err), OutcomeNotDispatched, result)
	}

	totalSleep := time.Duration(0)
	previousDelay := time.Duration(0)
	var undispatchedPermit resilience.Permit
	defer func() {
		if undispatchedPermit != nil {
			_ = undispatchedPermit.Complete()
		}
	}()
	workCtx, workAttempt, workPermit, workErr := policy.initialWorkContext(ctx)
	if workErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			result = finish(policy, start, result, ReasonCanceled)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeNotDispatched}, contextError(normalizedContext(contextErr), OutcomeNotDispatched, result)
		}
		result = finish(policy, start, result, ReasonWorkBudget)
		observe(policy, Observation{Elapsed: result.Elapsed, Reason: result.Reason})
		safe := singleCause("retry work admission failed", workErr)
		return ExecutionResult[T]{Retry: result, Outcome: OutcomeNotDispatched}, &BudgetError{Kind: BudgetWork, cause: safe, result: result.clone()}
	}
	undispatchedPermit = workPermit

	for attempt := uint(1); ; attempt++ {
		if err := ctx.Err(); err != nil {
			outcome := OutcomeNotDispatched
			if result.Attempts > 0 {
				outcome = OutcomeKnown
			}
			result = finish(policy, start, result, ReasonCanceled)
			return ExecutionResult[T]{Retry: result, Outcome: outcome}, contextError(normalizedContext(err), outcome, result)
		}
		if policy.config.MaxElapsed > 0 && elapsed(policy, start) >= policy.config.MaxElapsed {
			outcome := OutcomeNotDispatched
			if result.Attempts > 0 {
				outcome = OutcomeKnown
			}
			result = finish(policy, start, result, ReasonElapsedBudget)
			safe := singleCause("retry deadline reached", context.DeadlineExceeded)
			return ExecutionResult[T]{Retry: result, Outcome: outcome}, &BudgetError{Kind: BudgetElapsed, cause: safe, result: result.clone()}
		}

		attemptCtx, cancel, attemptBudget := policy.attemptContext(workCtx, start)
		// Until invocation, the outer defer owns completion. After transfer,
		// invokeOperation completes the permit even during panic unwinding.
		dispatchPermit := undispatchedPermit
		undispatchedPermit = nil
		attemptResult, operationErr, attemptErr := func() (AttemptResult[T], error, error) {
			defer cancel()
			result, err := invokeOperation(attemptCtx, operation, dispatchPermit)
			return result, err, attemptCtx.Err()
		}()
		result.Attempts = attempt

		if invalidOutcome(attemptResult.Outcome, operationErr) {
			result = appendHistory(policy, result, Attempt{Attempt: attempt, Elapsed: elapsed(policy, start)})
			result = finish(policy, start, result, ReasonOutcomeUnknown)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: ReasonOutcomeUnknown})
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeUnknown}, fmt.Errorf("%w: operation returned an invalid outcome", ErrInvalidPolicy)
		}
		if attemptResult.Outcome == OutcomeUnknown {
			result = appendHistory(policy, result, Attempt{Attempt: attempt, Elapsed: elapsed(policy, start)})
			result = finish(policy, start, result, ReasonOutcomeUnknown)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: ReasonOutcomeUnknown})
			executionResult := ExecutionResult[T]{Retry: result, Outcome: OutcomeUnknown}
			kind := unknownContextKind(ctx, operationErr)
			if kind != nil {
				return executionResult, contextError(kind, OutcomeUnknown, result)
			}
			return executionResult, outcomeError{}
		}
		if operationErr == nil {
			result = finish(policy, start, result, ReasonSucceeded)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: result.Reason})
			return ExecutionResult[T]{Value: attemptResult.Value, Retry: result, Outcome: OutcomeKnown}, nil
		}
		if attemptErr != nil && errors.Is(attemptErr, context.DeadlineExceeded) && ctx.Err() == nil {
			reason := ReasonAttemptBudget
			if attemptBudget == BudgetElapsed {
				reason = ReasonElapsedBudget
			}
			result = finish(policy, start, result, reason)
			safe := multipleCauses("retry attempt timed out", operationErr, context.DeadlineExceeded)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: attemptBudget, cause: safe, result: result.clone()}
		}

		classification, classifierPanicked, classifierErr := classify(ctx, policy.config.Classifier, operationErr)
		if classifierPanicked {
			result = finish(policy, start, result, ReasonClassifierFailure)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, fmt.Errorf("%w: classifier panicked", ErrInvalidPolicy)
		}
		if classifierErr != nil {
			result = finish(policy, start, result, ReasonClassifierFailure)
			safe := multipleCauses("retry classifier failed", operationErr, classifierErr)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		if classification != ClassificationRetryable && classification != ClassificationPermanent {
			result = finish(policy, start, result, ReasonClassifierFailure)
			safe := singleCause("retry classifier failed", operationErr)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		entry := Attempt{Attempt: attempt, Elapsed: elapsed(policy, start), Classification: classification, Err: operationErr}
		if classification == ClassificationPermanent {
			result = appendHistory(policy, result, entry)
			result = finish(policy, start, result, ReasonPermanent)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			safe := singleCause("retry operation failed", operationErr)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		if attempt == policy.config.MaxAttempts {
			result = appendHistory(policy, result, entry)
			result = finish(policy, start, result, ReasonAttemptsExhausted)
			safe := singleCause("retry operation failed", operationErr)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &ExhaustedError{cause: safe, result: result.clone()}
		}

		delay := policy.delay(attempt, previousDelay)
		var hint DelayHint
		if errors.As(operationErr, &hint) {
			if hinted, ok := hint.RetryDelay(policy.config.Clock.Now()); ok {
				delay = max(delay, policy.boundDelay(hinted))
			}
		}
		entry.Delay = delay
		result = appendHistory(policy, result, entry)
		result.FinalDelay = delay
		currentElapsed := elapsed(policy, start)
		if policy.config.MaxElapsed > 0 && (currentElapsed >= policy.config.MaxElapsed || delay > policy.config.MaxElapsed-currentElapsed) {
			result = finish(policy, start, result, ReasonElapsedBudget)
			safe := singleCause("retry operation failed", operationErr)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetElapsed, cause: safe, result: result.clone()}
		}
		if policy.config.MaxSleep > 0 && (totalSleep >= policy.config.MaxSleep || delay > policy.config.MaxSleep-totalSleep) {
			result = finish(policy, start, result, ReasonSleepBudget)
			safe := singleCause("retry operation failed", operationErr)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetSleep, cause: safe, result: result.clone()}
		}
		if policy.config.StopBeforeDeadline {
			if deadline, ok := ctx.Deadline(); ok && !policy.config.Clock.Now().Add(delay).Before(deadline) {
				result = finish(policy, start, result, ReasonDeadlineBudget)
				safe := singleCause("retry operation failed", operationErr)
				return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetDeadline, cause: safe, result: result.clone()}
			}
		}
		observe(policy, Observation{Attempt: attempt, Elapsed: currentElapsed, NextDelay: delay, Classification: classification})
		if err := policy.config.Sleeper.Sleep(ctx, delay); err != nil {
			if kind := sleeperContextKind(ctx, err); kind != nil {
				result = finish(policy, start, result, ReasonCanceled)
				return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, contextError(kind, OutcomeKnown, result)
			}
			result = finish(policy, start, result, ReasonSleeperFailure)
			safe := singleCause("retry sleeper failed", err)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		if err := ctx.Err(); err != nil {
			result = finish(policy, start, result, ReasonCanceled)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, contextError(normalizedContext(err), OutcomeKnown, result)
		}
		totalSleep = saturatingAdd(totalSleep, delay)
		previousDelay = delay
		nextWorkCtx, nextWorkAttempt, nextWorkPermit, admissionErr := policy.retryWorkContext(ctx, workAttempt)
		if admissionErr != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				result = finish(policy, start, result, ReasonCanceled)
				return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, contextError(normalizedContext(contextErr), OutcomeKnown, result)
			}
			result = finish(policy, start, result, ReasonWorkBudget)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			safe := singleCause("retry work admission failed", admissionErr)
			return ExecutionResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetWork, cause: safe, result: result.clone()}
		}
		workCtx = nextWorkCtx
		workAttempt = nextWorkAttempt
		undispatchedPermit = nextWorkPermit
	}

}

func invokeOperation[T any](ctx context.Context, operation Operation[T], permit resilience.Permit) (result AttemptResult[T], err error) {
	if permit != nil {
		defer func() { _ = permit.Complete() }()
	}
	return operation(ctx)
}

// Once a callback was invoked, NotDispatched cannot be its result. Unknown
// without failure cannot explain why success is unavailable.
func invalidOutcome(outcome Outcome, err error) bool {
	return outcome == OutcomeNotDispatched || (outcome == OutcomeUnknown && err == nil) || outcome > OutcomeUnknown
}

func classify(ctx context.Context, classifier Classifier, err error) (classification Classification, panicked bool, classifierErr error) {
	defer func() {
		if recover() != nil {
			classification = 0
			classifierErr = nil
			panicked = true
		}
	}()
	classification, classifierErr = classifier.Classify(ctx, err)
	return classification, false, classifierErr
}

func normalizedContext(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return context.Canceled
}

func unknownContextKind(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return normalizedContext(contextErr)
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func sleeperContextKind(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return normalizedContext(contextErr)
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func elapsed(policy *Policy, start time.Time) time.Duration {
	duration := policy.config.Clock.Now().Sub(start)
	return nonNegative(duration)
}

func finish(policy *Policy, start time.Time, result Result, reason Reason) Result {
	result.Elapsed = elapsed(policy, start)
	result.Reason = reason
	return result
}

// appendHistory keeps only the latest entries. Error references are retained,
// not deep-copied, so entry count is not a byte or disclosure budget.
func appendHistory(policy *Policy, result Result, attempt Attempt) Result {
	limit := policy.config.HistoryLimit
	if limit == 0 {
		return result
	}
	if uint(len(result.History)) == limit {
		copy(result.History, result.History[1:])
		result.History[len(result.History)-1] = attempt
		return result
	}
	result.History = append(result.History, attempt)
	return result
}

func observe(policy *Policy, observation Observation) {
	observer := policy.config.Observer
	if observer != nil {
		func() {
			defer func() { _ = recover() }()
			observer.Observe(observation)
		}()
	}
}
