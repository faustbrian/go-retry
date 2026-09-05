package retrytelemetry_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
	"github.com/faustbrian/go-retry/retrytelemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewStrictRejectsLiteralAndTypedNilProviderBeforeMeter(t *testing.T) {
	t.Parallel()

	var typedNil *strictNilMeterProvider
	for _, provider := range []metric.MeterProvider{nil, typedNil} {
		observer, err := retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: provider})
		if observer != nil || err == nil || err.Error() != "invalid retry policy: meter provider is required" || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("NewStrict = (%v,%v)", observer, err)
		}
	}
	if observer, err := retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: metricnoop.NewMeterProvider()}); observer == nil || err != nil {
		t.Fatalf("valid NewStrict = (%v,%v)", observer, err)
	}
}

func TestNewStrictRetainsExactLegacyMetricsScopeAndValidation(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: provider, PolicyID: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	observer.Observe(retry.Observation{Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second, Classification: retry.ClassificationRetryable, Reason: retry.ReasonSleepBudget})
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	if len(collected.ScopeMetrics) != 1 || collected.ScopeMetrics[0].Scope.Name != "github.com/faustbrian/go-retry/retrytelemetry" {
		t.Fatalf("scope metrics = %+v", collected.ScopeMetrics)
	}
	wantUnits := map[string]string{"retry.attempts": "{attempt}", "retry.elapsed": "s", "retry.delay": "s"}
	gotUnits := map[string]string{}
	gotValues := map[string]float64{}
	for _, measured := range collected.ScopeMetrics[0].Metrics {
		gotUnits[measured.Name] = measured.Unit
		switch data := measured.Data.(type) {
		case metricdata.Sum[int64]:
			if len(data.DataPoints) != 1 {
				t.Fatalf("%s data points = %d", measured.Name, len(data.DataPoints))
			}
			gotValues[measured.Name] = float64(data.DataPoints[0].Value)
			if got := strictAttributes(data.DataPoints[0].Attributes.ToSlice()); !reflect.DeepEqual(got, map[string]string{"retry.policy.id": "policy", "retry.classification": "retryable", "retry.reason": "sleep_budget"}) {
				t.Fatalf("%s attributes = %v", measured.Name, got)
			}
		case metricdata.Histogram[float64]:
			if len(data.DataPoints) != 1 || data.DataPoints[0].Count != 1 {
				t.Fatalf("%s histogram = %+v", measured.Name, data.DataPoints)
			}
			gotValues[measured.Name] = data.DataPoints[0].Sum
			if got := strictAttributes(data.DataPoints[0].Attributes.ToSlice()); !reflect.DeepEqual(got, map[string]string{"retry.policy.id": "policy", "retry.classification": "retryable", "retry.reason": "sleep_budget"}) {
				t.Fatalf("%s attributes = %v", measured.Name, got)
			}
		}
	}
	if !reflect.DeepEqual(gotUnits, wantUnits) || !reflect.DeepEqual(gotValues, map[string]float64{"retry.attempts": 1, "retry.elapsed": 3, "retry.delay": 1}) {
		t.Fatalf("units=%v values=%v", gotUnits, gotValues)
	}
	if observer, err := retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: metricnoop.NewMeterProvider(), PolicyID: strings.Repeat("x", retrytelemetry.MaxPolicyIDLength+1)}); observer != nil || err == nil || err.Error() != "invalid retry policy: policy ID exceeds 128 bytes" || !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("oversized policy ID = (%v,%v)", observer, err)
	}
}

func TestNewStrictPropagatesEveryInstrumentConstructionError(t *testing.T) {
	t.Parallel()

	want := errors.New("strict instrument failed")
	for _, meter := range []*errorMeter{
		{Meter: metricnoop.NewMeterProvider().Meter("test"), counterErr: want},
		{Meter: metricnoop.NewMeterProvider().Meter("test"), histogramErrs: []error{want}},
		{Meter: metricnoop.NewMeterProvider().Meter("test"), histogramErrs: []error{nil, want}},
	} {
		provider := errorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: meter}
		if observer, err := retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: provider}); observer != nil || !errors.Is(err, want) {
			t.Fatalf("NewStrict = (%v,%v)", observer, err)
		}
	}
}

func strictAttributes(values []attribute.KeyValue) map[string]string {
	result := map[string]string{}
	for _, value := range values {
		result[string(value.Key)] = value.Value.AsString()
	}
	return result
}

func TestLegacyNewRetainsTypedNilCompatibilityBehavior(t *testing.T) {
	t.Parallel()

	var typedNil *strictNilMeterProvider
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("legacy New did not invoke typed-nil provider")
		}
	}()
	_, _ = retrytelemetry.New(retrytelemetry.Options{MeterProvider: typedNil})
}

type strictNilMeterProvider struct{ metric.MeterProvider }

func (*strictNilMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	panic("legacy typed nil")
}
