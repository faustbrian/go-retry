package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
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
// Classified failures retain the original error object; strict unknown/invalid
// outcomes retain no error and have zero Classification and Delay.
type Attempt struct {
	// Attempt is the one-based physical callback count.
	Attempt uint
	// Elapsed is nonnegative clock-sampled time since execution began.
	Elapsed time.Duration
	// Delay is the selected next pause, even if a budget rejects it before sleep.
	Delay time.Duration
	// Classification is the accepted result, or zero for unclassified strict outcomes.
	Classification Classification
	// Err retains a classified operation error, or nil for unclassified strict outcomes.
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
	// Legacy Do can share this slice with its terminal error metadata; do not
	// mutate it if that metadata must remain stable. Error objects remain borrowed.
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
// invocation; policy construction treats it as an absent optional collaborator.
type ObserveFunc func(Observation)

// Observe invokes the adapted function.
func (function ObserveFunc) Observe(observation Observation) { function(observation) }

// Do executes operation under policy. The caller remains solely responsible
// for deciding whether repeating operation is safe. ctx must be non-nil;
// nil policy/operation returns ErrInvalidPolicy. Use a constructed policy.
// Calls are synchronous without operation-panic recovery. The caller context
// wins after an operation returns, including when that callback succeeded;
// failure paths return the zero T and may retain classified errors in history.
// Its returned History can alias stored terminal metadata; error Result accessors
// copy their current slice, not the referenced error objects.
// Attempt deadlines require callback cooperation and do not veto a nil error
// from an otherwise successful operation. DoStrict differs for known outcomes.
// Acquired work permits and attempt contexts are released on normal/error/panic
// exits; side effects are not rolled back and permit completion errors ignored.
func Do[T any](ctx context.Context, policy *Policy, operation func(context.Context) (T, error)) (T, Result, error) {
	var zero T
	if policy == nil || operation == nil {
		return zero, Result{}, fmt.Errorf("%w: policy and operation are required", ErrInvalidPolicy)
	}
	start := policy.config.Clock.Now()
	result := Result{}
	totalSleep := time.Duration(0)
	previousDelay := time.Duration(0)
	var undispatchedPermit budgetPermit
	defer func() {
		if undispatchedPermit != nil {
			_ = undispatchedPermit.Complete()
		}
	}()
	workCtx, workAttempt, workPermit, workErr := policy.initialWorkContext(ctx)
	if workErr != nil {
		result = finish(policy, start, result, ReasonWorkBudget)
		observe(policy, Observation{Elapsed: result.Elapsed, Reason: result.Reason})
		return zero, result, &BudgetError{Kind: BudgetWork, cause: workErr, result: result}
	}
	undispatchedPermit = workPermit

	for index := range policy.config.MaxAttempts {
		attempt := index + 1
		if err := ctx.Err(); err != nil {
			return zero, finish(policy, start, result, ReasonCanceled), &CanceledError{cause: err, result: finish(policy, start, result, ReasonCanceled)}
		}
		if policy.config.MaxElapsed > 0 && elapsed(policy, start) >= policy.config.MaxElapsed {
			result = finish(policy, start, result, ReasonElapsedBudget)
			return zero, result, &BudgetError{Kind: BudgetElapsed, cause: context.DeadlineExceeded, result: result}
		}

		attemptCtx, cancel, attemptBudget := policy.attemptContext(workCtx, start)
		// Transfer completion ownership only when entering the operation. The
		// outer defer owns an acquired permit while dispatch is still pending.
		dispatchPermit := undispatchedPermit
		undispatchedPermit = nil
		value, operationErr, attemptErr := func() (T, error, error) {
			// Keep cancel outside invokeOperation so permit-completion panic also
			// unwinds through attempt-context cleanup.
			defer cancel()
			value, err := invokeOperation(attemptCtx, operation, dispatchPermit)
			return value, err, attemptCtx.Err()
		}()
		result.Attempts = attempt

		if err := ctx.Err(); err != nil {
			result = finish(policy, start, result, ReasonCanceled)
			return zero, result, &CanceledError{cause: err, result: result}
		}
		if operationErr == nil {
			result = finish(policy, start, result, ReasonSucceeded)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: result.Reason})
			return value, result, nil
		}
		if attemptErr != nil && errors.Is(attemptErr, context.DeadlineExceeded) {
			reason := ReasonAttemptBudget
			if attemptBudget == BudgetElapsed {
				reason = ReasonElapsedBudget
			}
			result = finish(policy, start, result, reason)
			return zero, result, &BudgetError{Kind: attemptBudget, cause: errors.Join(operationErr, context.DeadlineExceeded), result: result}
		}

		classification, classifierErr := classify(ctx, policy.config.Classifier, operationErr)
		if classifierErr != nil {
			result = finish(policy, start, result, ReasonClassifierFailure)
			return zero, result, &PermanentError{Cause: errors.Join(operationErr, classifierErr)}
		}
		if classification != ClassificationRetryable && classification != ClassificationPermanent {
			result = finish(policy, start, result, ReasonClassifierFailure)
			return zero, result, &PermanentError{Cause: strictSingleCause("retry classifier returned invalid classification", operationErr)}
		}
		entry := Attempt{Attempt: attempt, Elapsed: elapsed(policy, start), Classification: classification, Err: operationErr}
		if classification == ClassificationPermanent {
			result = appendHistory(policy, result, entry)
			result = finish(policy, start, result, ReasonPermanent)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			return zero, result, &PermanentError{Cause: operationErr}
		}
		if attempt == policy.config.MaxAttempts {
			result = appendHistory(policy, result, entry)
			result = finish(policy, start, result, ReasonAttemptsExhausted)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			return zero, result, &ExhaustedError{cause: operationErr, result: result}
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
			return zero, result, &BudgetError{Kind: BudgetElapsed, cause: operationErr, result: result}
		}
		if policy.config.MaxSleep > 0 && (totalSleep >= policy.config.MaxSleep || delay > policy.config.MaxSleep-totalSleep) {
			result = finish(policy, start, result, ReasonSleepBudget)
			return zero, result, &BudgetError{Kind: BudgetSleep, cause: operationErr, result: result}
		}
		observe(policy, Observation{Attempt: attempt, Elapsed: currentElapsed, NextDelay: delay, Classification: classification})
		if err := policy.config.Sleeper.Sleep(ctx, delay); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				result = finish(policy, start, result, ReasonCanceled)
				return zero, result, &CanceledError{cause: err, result: result}
			}
			result = finish(policy, start, result, ReasonSleeperFailure)
			return zero, result, &PermanentError{Cause: err}
		}
		totalSleep = saturatingAdd(totalSleep, delay)
		previousDelay = delay
		nextWorkCtx, nextWorkAttempt, nextWorkPermit, admissionErr := policy.retryWorkContext(ctx, workAttempt)
		if admissionErr != nil {
			result = finish(policy, start, result, ReasonWorkBudget)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			return zero, result, &BudgetError{Kind: BudgetWork, cause: admissionErr, result: result}
		}
		workCtx = nextWorkCtx
		workAttempt = nextWorkAttempt
		undispatchedPermit = nextWorkPermit
	}

	return zero, result, fmt.Errorf("%w: retry loop ended without a terminal result", ErrInvalidPolicy)
}

func invokeOperation[T any](ctx context.Context, operation func(context.Context) (T, error), permit budgetPermit) (value T, err error) {
	if permit != nil {
		defer func() { _ = permit.Complete() }()
	}
	return operation(ctx)
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

func classify(ctx context.Context, classifier Classifier, err error) (classification Classification, classifierErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if cause, ok := recovered.(error); ok {
				classifierErr = strictSingleCause("retry classifier panicked", cause)
			} else {
				classifierErr = errors.New("retry classifier panicked")
			}
		}
	}()
	return classifier.Classify(ctx, err)
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
