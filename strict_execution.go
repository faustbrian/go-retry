package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/faustbrian/go-resilience"
)

// ReasonOutcomeUnknown identifies a dispatched operation whose result cannot
// be proved.
const ReasonOutcomeUnknown Reason = "outcome_unknown"

// AttemptResult declares the value and side-effect outcome of one attempt.
type AttemptResult[T any] struct {
	Value   T
	Outcome Outcome
}

// StrictResult contains a value only for known success, bounded retry
// metadata, and the operation dispatch outcome.
type StrictResult[T any] struct {
	Value   T
	Retry   Result
	Outcome Outcome
}

// StrictOperation executes one attempt and declares whether its outcome is
// known. It must never label an ambiguous side effect as known.
type StrictOperation[T any] func(context.Context) (AttemptResult[T], error)

// DoStrict executes operation under policy without retrying an unknown outcome
// and returns only bounded human-readable terminal errors.
func DoStrict[T any](ctx context.Context, policy *Policy, operation StrictOperation[T]) (StrictResult[T], error) {
	if nilLike(ctx) || policy == nil || operation == nil {
		return StrictResult[T]{}, fmt.Errorf("%w: context, policy, and operation are required", ErrInvalidPolicy)
	}

	start := policy.config.Clock.Now()
	result := Result{}
	if err := ctx.Err(); err != nil {
		result = finish(policy, start, result, ReasonCanceled)
		return StrictResult[T]{Retry: result, Outcome: OutcomeNotDispatched}, strictContextError(normalizedContext(err), OutcomeNotDispatched, result)
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
			return StrictResult[T]{Retry: result, Outcome: OutcomeNotDispatched}, strictContextError(normalizedContext(contextErr), OutcomeNotDispatched, result)
		}
		result = finish(policy, start, result, ReasonWorkBudget)
		observe(policy, Observation{Elapsed: result.Elapsed, Reason: result.Reason})
		safe := strictSingleCause("retry work admission failed", workErr)
		return StrictResult[T]{Retry: result, Outcome: OutcomeNotDispatched}, &BudgetError{Kind: BudgetWork, cause: safe, result: result.clone()}
	}
	undispatchedPermit = workPermit

	for attempt := uint(1); ; attempt++ {
		if err := ctx.Err(); err != nil {
			outcome := OutcomeNotDispatched
			if result.Attempts > 0 {
				outcome = OutcomeKnown
			}
			result = finish(policy, start, result, ReasonCanceled)
			return StrictResult[T]{Retry: result, Outcome: outcome}, strictContextError(normalizedContext(err), outcome, result)
		}
		if policy.config.MaxElapsed > 0 && elapsed(policy, start) >= policy.config.MaxElapsed {
			outcome := OutcomeNotDispatched
			if result.Attempts > 0 {
				outcome = OutcomeKnown
			}
			result = finish(policy, start, result, ReasonElapsedBudget)
			safe := strictSingleCause("retry deadline reached", context.DeadlineExceeded)
			return StrictResult[T]{Retry: result, Outcome: outcome}, &BudgetError{Kind: BudgetElapsed, cause: safe, result: result.clone()}
		}

		attemptCtx, cancel, attemptBudget := policy.attemptContext(workCtx, start)
		dispatchPermit := undispatchedPermit
		undispatchedPermit = nil
		attemptResult, operationErr, attemptErr := func() (AttemptResult[T], error, error) {
			defer cancel()
			result, err := invokeStrictOperation(attemptCtx, operation, dispatchPermit)
			return result, err, attemptCtx.Err()
		}()
		result.Attempts = attempt

		if invalidStrictOutcome(attemptResult.Outcome, operationErr) {
			result = appendHistory(policy, result, Attempt{Attempt: attempt, Elapsed: elapsed(policy, start)})
			result = finish(policy, start, result, ReasonOutcomeUnknown)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: ReasonOutcomeUnknown})
			return StrictResult[T]{Retry: result, Outcome: OutcomeUnknown}, fmt.Errorf("%w: strict operation returned an invalid outcome", ErrInvalidPolicy)
		}
		if attemptResult.Outcome == OutcomeUnknown {
			result = appendHistory(policy, result, Attempt{Attempt: attempt, Elapsed: elapsed(policy, start)})
			result = finish(policy, start, result, ReasonOutcomeUnknown)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: ReasonOutcomeUnknown})
			strictResult := StrictResult[T]{Retry: result, Outcome: OutcomeUnknown}
			kind := unknownContextKind(ctx, operationErr)
			if kind != nil {
				return strictResult, strictContextError(kind, OutcomeUnknown, result)
			}
			return strictResult, strictOutcomeError{}
		}
		if operationErr == nil {
			result = finish(policy, start, result, ReasonSucceeded)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Reason: result.Reason})
			return StrictResult[T]{Value: attemptResult.Value, Retry: result, Outcome: OutcomeKnown}, nil
		}
		if attemptErr != nil && errors.Is(attemptErr, context.DeadlineExceeded) && ctx.Err() == nil {
			reason := ReasonAttemptBudget
			if attemptBudget == BudgetElapsed {
				reason = ReasonElapsedBudget
			}
			result = finish(policy, start, result, reason)
			safe := strictMultipleCauses("retry attempt timed out", operationErr, context.DeadlineExceeded)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: attemptBudget, cause: safe, result: result.clone()}
		}

		classification, classifierPanicked, classifierErr := classifyStrict(ctx, policy.config.Classifier, operationErr)
		if classifierPanicked {
			result = finish(policy, start, result, ReasonClassifierFailure)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, fmt.Errorf("%w: classifier panicked", ErrInvalidPolicy)
		}
		if classifierErr != nil {
			result = finish(policy, start, result, ReasonClassifierFailure)
			safe := strictMultipleCauses("retry classifier failed", operationErr, classifierErr)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		if classification != ClassificationRetryable && classification != ClassificationPermanent {
			result = finish(policy, start, result, ReasonClassifierFailure)
			safe := strictSingleCause("retry classifier failed", operationErr)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		entry := Attempt{Attempt: attempt, Elapsed: elapsed(policy, start), Classification: classification, Err: operationErr}
		if classification == ClassificationPermanent {
			result = appendHistory(policy, result, entry)
			result = finish(policy, start, result, ReasonPermanent)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			safe := strictSingleCause("retry operation failed", operationErr)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		if attempt == policy.config.MaxAttempts {
			result = appendHistory(policy, result, entry)
			result = finish(policy, start, result, ReasonAttemptsExhausted)
			safe := strictSingleCause("retry operation failed", operationErr)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &ExhaustedError{cause: safe, result: result.clone()}
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
			safe := strictSingleCause("retry operation failed", operationErr)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetElapsed, cause: safe, result: result.clone()}
		}
		if policy.config.MaxSleep > 0 && (totalSleep >= policy.config.MaxSleep || delay > policy.config.MaxSleep-totalSleep) {
			result = finish(policy, start, result, ReasonSleepBudget)
			safe := strictSingleCause("retry operation failed", operationErr)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetSleep, cause: safe, result: result.clone()}
		}
		observe(policy, Observation{Attempt: attempt, Elapsed: currentElapsed, NextDelay: delay, Classification: classification})
		if err := policy.config.Sleeper.Sleep(ctx, delay); err != nil {
			if kind := sleeperContextKind(ctx, err); kind != nil {
				result = finish(policy, start, result, ReasonCanceled)
				return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, strictContextError(kind, OutcomeKnown, result)
			}
			result = finish(policy, start, result, ReasonSleeperFailure)
			safe := strictSingleCause("retry sleeper failed", err)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &PermanentError{Cause: safe}
		}
		if err := ctx.Err(); err != nil {
			result = finish(policy, start, result, ReasonCanceled)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, strictContextError(normalizedContext(err), OutcomeKnown, result)
		}
		totalSleep = saturatingAdd(totalSleep, delay)
		previousDelay = delay
		nextWorkCtx, nextWorkAttempt, nextWorkPermit, admissionErr := policy.retryWorkContext(ctx, workAttempt)
		if admissionErr != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				result = finish(policy, start, result, ReasonCanceled)
				return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, strictContextError(normalizedContext(contextErr), OutcomeKnown, result)
			}
			result = finish(policy, start, result, ReasonWorkBudget)
			observe(policy, Observation{Attempt: attempt, Elapsed: result.Elapsed, Classification: classification, Reason: result.Reason})
			safe := strictSingleCause("retry work admission failed", admissionErr)
			return StrictResult[T]{Retry: result, Outcome: OutcomeKnown}, &BudgetError{Kind: BudgetWork, cause: safe, result: result.clone()}
		}
		workCtx = nextWorkCtx
		workAttempt = nextWorkAttempt
		undispatchedPermit = nextWorkPermit
	}

}

func invokeStrictOperation[T any](ctx context.Context, operation StrictOperation[T], permit resilience.Permit) (result AttemptResult[T], err error) {
	if permit != nil {
		defer func() { _ = permit.Complete() }()
	}
	return operation(ctx)
}

func invalidStrictOutcome(outcome Outcome, err error) bool {
	return outcome == OutcomeNotDispatched || (outcome == OutcomeUnknown && err == nil) || outcome > OutcomeUnknown
}

func classifyStrict(ctx context.Context, classifier Classifier, err error) (classification Classification, panicked bool, classifierErr error) {
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
