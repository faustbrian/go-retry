// Package retrytelemetry adapts bounded retry observations to the standard
// OpenTelemetry API accepted by telemetry.
package retrytelemetry

import (
	"context"
	"fmt"

	retry "github.com/faustbrian/golib/pkg/retry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const scopeName = "github.com/faustbrian/golib/pkg/retry/retrytelemetry"

// MaxPolicyIDLength bounds the caller-supplied metric attribute.
const MaxPolicyIDLength = 128

// Options configures retry metric instruments.
type Options struct {
	MeterProvider metric.MeterProvider
	PolicyID      string
}

// Observer records attempt counts, elapsed time, and selected delays.
type Observer struct {
	policyID string
	attempts metric.Int64Counter
	elapsed  metric.Float64Histogram
	delay    metric.Float64Histogram
}

// New constructs bounded retry metric instruments.
func New(options Options) (*Observer, error) {
	if options.MeterProvider == nil {
		return nil, fmt.Errorf("%w: meter provider is required", retry.ErrInvalidPolicy)
	}
	if len(options.PolicyID) > MaxPolicyIDLength {
		return nil, fmt.Errorf("%w: policy ID exceeds %d bytes", retry.ErrInvalidPolicy, MaxPolicyIDLength)
	}
	meter := options.MeterProvider.Meter(scopeName)
	attempts, err := meter.Int64Counter("retry.attempts", metric.WithUnit("{attempt}"))
	if err != nil {
		return nil, err
	}
	elapsed, err := meter.Float64Histogram("retry.elapsed", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	delay, err := meter.Float64Histogram("retry.delay", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	return &Observer{policyID: options.PolicyID, attempts: attempts, elapsed: elapsed, delay: delay}, nil
}

// Observe records bounded attributes and no operation values or errors.
func (observer *Observer) Observe(observation retry.Observation) {
	attributes := metric.WithAttributes(
		attribute.String("retry.policy.id", observer.policyID),
		attribute.String("retry.classification", classification(observation.Classification)),
		attribute.String("retry.reason", reason(observation.Reason)),
	)
	ctx := context.Background()
	observer.attempts.Add(ctx, 1, attributes)
	observer.elapsed.Record(ctx, observation.Elapsed.Seconds(), attributes)
	observer.delay.Record(ctx, observation.NextDelay.Seconds(), attributes)
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
		retry.ReasonAttemptBudget, retry.ReasonClassifierFailure, retry.ReasonSleeperFailure:
		return string(value)
	default:
		return "unknown"
	}
}

var _ retry.Observer = (*Observer)(nil)
