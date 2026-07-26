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
)

// Attempt records bounded failure metadata. It never retains operation values.
type Attempt struct {
	Attempt        uint
	Elapsed        time.Duration
	Delay          time.Duration
	Classification Classification
	Err            error
}

// Result contains bounded execution metadata and never retains operation
// values.
type Result struct {
	Attempts   uint
	Elapsed    time.Duration
	FinalDelay time.Duration
	Reason     Reason
	History    []Attempt
}

// DelayHint is implemented by classified errors that carry a server-provided
// minimum retry delay, such as HTTP Retry-After.
type DelayHint interface {
	RetryDelay(time.Time) (time.Duration, bool)
}

func (result Result) clone() Result {
	result.History = append([]Attempt(nil), result.History...)
	return result
}

// Observer receives bounded lifecycle metadata.
type Observer interface {
	Observe(Observation)
}

// Observation is a bounded notification for one completed attempt.
type Observation struct {
	Attempt        uint
	Elapsed        time.Duration
	NextDelay      time.Duration
	Classification Classification
	Reason         Reason
}

// ObserveFunc adapts a function to Observer.
type ObserveFunc func(Observation)

// Observe invokes the adapted function.
func (function ObserveFunc) Observe(observation Observation) { function(observation) }

// Do executes operation under policy. The caller remains solely responsible
// for deciding whether repeating operation is safe.
func Do[T any](ctx context.Context, policy *Policy, operation func(context.Context) (T, error)) (T, Result, error) {
	var zero T
	if policy == nil || operation == nil {
		return zero, Result{}, fmt.Errorf("%w: policy and operation are required", ErrInvalidPolicy)
	}
	start := policy.config.Clock.Now()
	result := Result{}
	totalSleep := time.Duration(0)
	previousDelay := time.Duration(0)

	for attempt := uint(1); ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, finish(policy, start, result, ReasonCanceled), &CanceledError{cause: err, result: finish(policy, start, result, ReasonCanceled)}
		}
		if policy.config.MaxElapsed > 0 && elapsed(policy, start) >= policy.config.MaxElapsed {
			result = finish(policy, start, result, ReasonElapsedBudget)
			return zero, result, &BudgetError{Kind: BudgetElapsed, cause: context.DeadlineExceeded, result: result}
		}

		attemptCtx, cancel, attemptBudget := policy.attemptContext(ctx, start)
		value, operationErr := operation(attemptCtx)
		attemptErr := attemptCtx.Err()
		cancel()
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
			return zero, result, &PermanentError{Cause: fmt.Errorf("classifier returned invalid classification %d: %w", classification, operationErr)}
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
			if hinted, ok := hint.RetryDelay(policy.config.Clock.Now()); ok && hinted > delay {
				delay = policy.boundDelay(hinted)
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
	}
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
			classifierErr = fmt.Errorf("classifier panic: %v", recovered)
		}
	}()
	return classifier.Classify(ctx, err)
}

func observe(policy *Policy, observation Observation) {
	if policy.config.Observer == nil {
		return
	}
	defer func() { _ = recover() }()
	policy.config.Observer.Observe(observation)
}
