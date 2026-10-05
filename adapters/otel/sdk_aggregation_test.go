package retryotel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v2"
	retryotel "github.com/faustbrian/go-retry/v2/adapters/otel"

	//lint:ignore SA1019 Legacy parity is the compatibility contract under test.
	"github.com/faustbrian/go-retry/v2/retrytelemetry" //nolint:staticcheck // Legacy parity is the compatibility contract under test.
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSDKFilteredViewsMergePoliciesWithoutLosingMeasurements(t *testing.T) {
	t.Parallel()

	for _, variant := range []struct {
		name, scope string
		newObserver func(metric.MeterProvider, string) (retry.Observer, error)
	}{
		{"legacy", "github.com/faustbrian/go-retry/retrytelemetry", func(provider metric.MeterProvider, policy string) (retry.Observer, error) {
			return retrytelemetry.New(retrytelemetry.Options{MeterProvider: provider, PolicyID: policy})
		}},
		{"strict legacy", "github.com/faustbrian/go-retry/retrytelemetry", func(provider metric.MeterProvider, policy string) (retry.Observer, error) {
			return retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: provider, PolicyID: policy})
		}},
		{"successor", "github.com/faustbrian/go-retry/v2/adapters/otel", func(provider metric.MeterProvider, policy string) (retry.Observer, error) {
			return retryotel.New(retryotel.Options{MeterProvider: provider, PolicyID: policy})
		}},
	} {
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			view := sdkmetric.NewView(sdkmetric.Instrument{Name: "retry.*"}, sdkmetric.Stream{AttributeFilter: func(value attribute.KeyValue) bool {
				return value.Key != "retry.policy.id"
			}})
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithView(view))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			first, err := variant.newObserver(provider, "alpha")
			if err != nil {
				t.Fatal(err)
			}
			second, err := variant.newObserver(provider, "bravo")
			if err != nil {
				t.Fatal(err)
			}
			observe := func(observer retry.Observer, elapsed, delay time.Duration) {
				observer.Observe(retry.Observation{Attempt: 2, Elapsed: elapsed, NextDelay: delay, Classification: retry.ClassificationRetryable, Reason: retry.ReasonSucceeded})
			}
			observe(first, 250*time.Millisecond, 0)
			observe(second, 750*time.Millisecond, 125*time.Millisecond)
			collect := func(count int64, histogramCount uint64, elapsed, delay float64) {
				t.Helper()
				var data metricdata.ResourceMetrics
				if collectErr := reader.Collect(context.Background(), &data); collectErr != nil {
					t.Fatal(collectErr)
				}
				if len(data.ScopeMetrics) != 1 || data.ScopeMetrics[0].Scope.Name != variant.scope || len(data.ScopeMetrics[0].Metrics) != 3 {
					t.Fatalf("scope metrics = %+v", data.ScopeMetrics)
				}
				wantAttributes := attribute.NewSet(attribute.String("retry.classification", "retryable"), attribute.String("retry.reason", "succeeded"))
				seen := make(map[string]bool)
				for _, measured := range data.ScopeMetrics[0].Metrics {
					if seen[measured.Name] {
						t.Fatalf("duplicate metric %q", measured.Name)
					}
					seen[measured.Name] = true
					if measured.Name == "retry.attempts" {
						sum, ok := measured.Data.(metricdata.Sum[int64])
						if !ok || measured.Unit != "{attempt}" || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != count || !sum.DataPoints[0].Attributes.Equals(&wantAttributes) {
							t.Fatalf("attempt metric = %+v", measured)
						}
						continue
					}
					wantSum := elapsed
					if measured.Name == "retry.delay" {
						wantSum = delay
					} else if measured.Name != "retry.elapsed" {
						t.Fatalf("unexpected metric %q", measured.Name)
					}
					histogram, ok := measured.Data.(metricdata.Histogram[float64])
					if !ok || measured.Unit != "s" || len(histogram.DataPoints) != 1 || histogram.DataPoints[0].Count != histogramCount || histogram.DataPoints[0].Sum != wantSum || !histogram.DataPoints[0].Attributes.Equals(&wantAttributes) {
						t.Fatalf("histogram = %+v", measured)
					}
				}
			}
			collect(2, 2, 1, 0.125)
			var joined sync.WaitGroup
			for index := range 32 {
				joined.Go(func() {
					observer := first
					if index%2 != 0 {
						observer = second
					}
					observe(observer, 250*time.Millisecond, 125*time.Millisecond)
				})
			}
			joined.Wait()
			collect(34, 34, 9, 4.125)
		})
	}
}
