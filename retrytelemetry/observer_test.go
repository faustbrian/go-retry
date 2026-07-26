package retrytelemetry_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/faustbrian/golib/pkg/retry/retrytelemetry"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestObserverRecordsBoundedRetryMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := retrytelemetry.New(retrytelemetry.Options{MeterProvider: provider, PolicyID: "invoice-read"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	observer.Observe(retry.Observation{
		Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second,
		Classification: retry.ClassificationRetryable, Reason: retry.ReasonSleepBudget,
	})
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(collected.ScopeMetrics) != 1 || len(collected.ScopeMetrics[0].Metrics) != 3 {
		t.Fatalf("metrics = %+v", collected.ScopeMetrics)
	}
	for _, metric := range collected.ScopeMetrics[0].Metrics {
		if metric.Name != "retry.attempts" && metric.Name != "retry.elapsed" && metric.Name != "retry.delay" {
			t.Fatalf("unexpected metric %q", metric.Name)
		}
	}
}

func TestObserverPropagatesInstrumentConstructionErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument failed")
	tests := []struct {
		name  string
		meter *errorMeter
	}{
		{"counter", &errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), counterErr: want}},
		{"elapsed histogram", &errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), histogramErrs: []error{want}}},
		{"delay histogram", &errorMeter{Meter: metricnoop.NewMeterProvider().Meter("test"), histogramErrs: []error{nil, want}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := errorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: test.meter}
			if _, err := retrytelemetry.New(retrytelemetry.Options{MeterProvider: provider}); !errors.Is(err, want) {
				t.Fatalf("New error = %v, want instrument error", err)
			}
		})
	}
}

func TestObserverRejectsMissingProviderAndUnboundedPolicyID(t *testing.T) {
	t.Parallel()

	if _, err := retrytelemetry.New(retrytelemetry.Options{}); !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("missing provider error = %v", err)
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	if _, err := retrytelemetry.New(retrytelemetry.Options{MeterProvider: provider, PolicyID: strings.Repeat("x", retrytelemetry.MaxPolicyIDLength+1)}); !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("long policy ID error = %v", err)
	}
}

func TestObserverBoundsEveryEnumValue(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := retrytelemetry.New(retrytelemetry.Options{MeterProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	for _, classification := range []retry.Classification{0, retry.ClassificationPermanent, retry.ClassificationRetryable, 99} {
		observer.Observe(retry.Observation{Classification: classification})
	}
	for _, reason := range []retry.Reason{"", retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted,
		retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget, retry.ReasonAttemptBudget,
		retry.ReasonClassifierFailure, retry.ReasonSleeperFailure, "hostile"} {
		observer.Observe(retry.Observation{Reason: reason})
	}
}

type errorMeterProvider struct {
	metric.MeterProvider
	meter *errorMeter
}

func (provider errorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type errorMeter struct {
	metric.Meter
	counterErr    error
	histogramErrs []error
	histogramCall int
}

func (meter *errorMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return meter.Meter.Int64Counter(name, options...)
}

func (meter *errorMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if meter.histogramCall < len(meter.histogramErrs) {
		err := meter.histogramErrs[meter.histogramCall]
		meter.histogramCall++
		if err != nil {
			return nil, err
		}
	}
	return meter.Meter.Float64Histogram(name, options...)
}
