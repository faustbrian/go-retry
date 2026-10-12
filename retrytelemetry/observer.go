// Package retrytelemetry adapts bounded retry observations to OpenTelemetry
// metrics. Start with NewStrict to reject
// typed-nil providers while retaining the legacy instrumentation scope.
// Providers/instruments are borrowed, calls synchronous, and nil/zero observers
// retain panic behavior. The package owns no exporter or shutdown lifecycle.
//
// Deprecated: use github.com/faustbrian/go-retry/v2/adapters/otel. This package
// remains supported through the documented compatibility interval.
package retrytelemetry

import (
	"context"
	"fmt"
	"reflect"

	retry "github.com/faustbrian/go-retry/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Retain the released instrumentation identity across the Go module migration.
const scopeName = "github.com/faustbrian/go-retry/retrytelemetry"

// MaxPolicyIDLength bounds the caller-supplied metric attribute.
const MaxPolicyIDLength = 128

// Options configures retry metric instruments.
type Options struct {
	// MeterProvider is required and borrowed; the caller owns readers, exporters
	// and shutdown after all observer calls finish.
	MeterProvider metric.MeterProvider
	// PolicyID is optional and byte-bounded, not sanitized or cardinality-limited.
	// Use a stable non-sensitive policy label.
	PolicyID string
}

// Observer counts received observations and records cumulative elapsed time and
// selected delays per notification. retry.attempts is not a physical-dispatch
// counter: retry execution can omit events or report early admission denial.
// The observer borrows instruments/provider resources and owns no shutdown.
type Observer struct {
	policyID string
	attempts metric.Int64Counter
	elapsed  metric.Float64Histogram
	delay    metric.Float64Histogram
}

// New constructs instruments synchronously under the retained legacy scope.
// It rejects a nil interface, but a typed-nil provider can be invoked and panic;
// use NewStrict to reject that case. Errors propagate without partial rollback.
// No provider/reader/exporter is closed by this package.
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

// NewStrict constructs bounded retry metric instruments and rejects an
// explicitly supplied typed-nil meter provider before invoking it.
func NewStrict(options Options) (*Observer, error) {
	if nilMeterProvider(options.MeterProvider) {
		return nil, fmt.Errorf("%w: meter provider is required", retry.ErrInvalidPolicy)
	}
	return New(options)
}

func nilMeterProvider(provider metric.MeterProvider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	//nolint:exhaustive // Only kinds that can contain nil require a case.
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Observe records bounded attributes and no operation values or errors. A nil
// or zero observer panics, unlike the canonical observer. Calls use Background
// and can block, overlap or reenter; direct sink panics propagate. Legacy
// mapping renders ReasonOutcomeUnknown as unknown rather than outcome_unknown.
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
	//nolint:exhaustive // Legacy observers intentionally map new reasons through the fallback.
	switch value {
	case "":
		return "none"
	case retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted,
		retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget,
		retry.ReasonAttemptBudget, retry.ReasonClassifierFailure, retry.ReasonSleeperFailure,
		retry.ReasonWorkBudget:
		return string(value)
	default:
		return "unknown"
	}
}

var _ retry.Observer = (*Observer)(nil)
