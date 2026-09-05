package retryslog

import (
	"context"
	"fmt"
	"log/slog"

	retry "github.com/faustbrian/go-retry"
)

// MaxPolicyIDLength bounds the caller-supplied policy identity.
const MaxPolicyIDLength = 128

// Options configures structured retry observation logging.
type Options struct {
	Logger   *slog.Logger
	Level    slog.Level
	PolicyID string
}

// Observer logs bounded retry lifecycle fields synchronously.
type Observer struct {
	logger   *slog.Logger
	level    slog.Level
	policyID string
}

// New validates options and constructs an immutable Observer.
func New(options Options) (*Observer, error) {
	if options.Logger == nil {
		return nil, fmt.Errorf("%w: logger is required", retry.ErrInvalidPolicy)
	}
	if len(options.PolicyID) > MaxPolicyIDLength {
		return nil, fmt.Errorf("%w: policy ID exceeds %d bytes", retry.ErrInvalidPolicy, MaxPolicyIDLength)
	}
	return &Observer{logger: options.Logger, level: options.Level, policyID: options.PolicyID}, nil
}

// Observe records one event without operation errors or values. A nil or zero
// observer is a no-op.
func (observer *Observer) Observe(observation retry.Observation) {
	if observer == nil || observer.logger == nil {
		return
	}
	observer.logger.LogAttrs(
		context.Background(), observer.level, "retry observation",
		slog.String("policy_id", observer.policyID),
		slog.Uint64("attempt", uint64(observation.Attempt)),
		slog.Int64("elapsed_ns", observation.Elapsed.Nanoseconds()),
		slog.Int64("next_delay_ns", observation.NextDelay.Nanoseconds()),
		slog.String("classification", classification(observation.Classification)),
		slog.String("reason", reason(observation.Reason)),
	)
}

func classification(value retry.Classification) string {
	switch value {
	case 0:
		return "none"
	case retry.ClassificationPermanent:
		return "permanent"
	case retry.ClassificationRetryable:
		return "retryable"
	default:
		return "unknown"
	}
}

func reason(value retry.Reason) string {
	switch value {
	case "":
		return "none"
	case retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted,
		retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget,
		retry.ReasonAttemptBudget, retry.ReasonClassifierFailure, retry.ReasonSleeperFailure,
		retry.ReasonWorkBudget, retry.ReasonOutcomeUnknown:
		return string(value)
	default:
		return "unknown"
	}
}

var _ retry.Observer = (*Observer)(nil)
