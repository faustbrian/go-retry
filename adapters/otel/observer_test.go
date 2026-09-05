package retryotel_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
	retryotel "github.com/faustbrian/go-retry/adapters/otel"

	//lint:ignore SA1019 Legacy parity is the compatibility contract under test.
	"github.com/faustbrian/go-retry/retrytelemetry" //nolint:staticcheck // Legacy parity is the compatibility contract under test.
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewRejectsLiteralAndTypedNilProviderBeforeMeter(t *testing.T) {
	t.Parallel()

	var typedNil *nilMeterProvider
	for _, provider := range []metric.MeterProvider{nil, typedNil} {
		observer, err := retryotel.New(retryotel.Options{MeterProvider: provider})
		if observer != nil || err == nil || err.Error() != "invalid retry policy: meter provider is required" || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("New = (%v,%v)", observer, err)
		}
	}
}

func TestObserverRecordsExactMetricsUnderSuccessorScope(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := retryotel.New(retryotel.Options{MeterProvider: provider, PolicyID: "invoice-read"})
	if err != nil {
		t.Fatal(err)
	}
	observer.Observe(retry.Observation{
		Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second,
		Classification: retry.ClassificationRetryable, Reason: retry.ReasonOutcomeUnknown,
	})
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	if len(collected.ScopeMetrics) != 1 || collected.ScopeMetrics[0].Scope.Name != "github.com/faustbrian/go-retry/adapters/otel" || len(collected.ScopeMetrics[0].Metrics) != 3 {
		t.Fatalf("scope metrics = %+v", collected.ScopeMetrics)
	}
	for _, measured := range collected.ScopeMetrics[0].Metrics {
		if measured.Name != "retry.attempts" && measured.Name != "retry.elapsed" && measured.Name != "retry.delay" {
			t.Fatalf("metric = %q", measured.Name)
		}
	}
}

func TestObserverMetricsMatchLegacyWithExactValuesUnitsAndMappings(t *testing.T) {
	t.Parallel()

	classifications := map[retry.Classification]string{
		0: "none", retry.ClassificationPermanent: "permanent",
		retry.ClassificationRetryable: "retryable", 99: "unknown",
	}
	for classification, want := range classifications {
		observation := retry.Observation{Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second, Classification: classification}
		successor := collectSnapshot(t, observation, false)
		compatibility := collectSnapshot(t, observation, true)
		if successor.attributes["retry.attempts"]["retry.classification"] != want || !equalMetricContent(successor, compatibility) {
			t.Fatalf("classification %d successor=%+v legacy=%+v", classification, successor, compatibility)
		}
	}
	reasons := map[retry.Reason]string{
		"": "none", retry.ReasonSucceeded: "succeeded", retry.ReasonPermanent: "permanent",
		retry.ReasonAttemptsExhausted: "attempts_exhausted", retry.ReasonCanceled: "canceled",
		retry.ReasonElapsedBudget: "elapsed_budget", retry.ReasonSleepBudget: "sleep_budget",
		retry.ReasonAttemptBudget: "attempt_budget", retry.ReasonClassifierFailure: "classifier_failure",
		retry.ReasonSleeperFailure: "sleeper_failure", retry.ReasonWorkBudget: "work_budget", "hostile": "unknown",
	}
	for reason, want := range reasons {
		observation := retry.Observation{Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second, Reason: reason}
		successor := collectSnapshot(t, observation, false)
		compatibility := collectSnapshot(t, observation, true)
		if successor.attributes["retry.attempts"]["retry.reason"] != want || !equalMetricContent(successor, compatibility) {
			t.Fatalf("reason %q successor=%+v legacy=%+v", reason, successor, compatibility)
		}
	}
	unknown := collectSnapshot(t, retry.Observation{Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second, Reason: retry.ReasonOutcomeUnknown}, false)
	if unknown.attributes["retry.attempts"]["retry.reason"] != "outcome_unknown" {
		t.Fatalf("outcome unknown attributes = %v", unknown.attributes)
	}
}

func TestLegacyAndStrictLegacyObserversRetainExactScopeAndMetrics(t *testing.T) {
	t.Parallel()

	observation := retry.Observation{Attempt: 2, Elapsed: 3 * time.Second, NextDelay: time.Second, Classification: retry.ClassificationRetryable, Reason: retry.ReasonSleepBudget}
	legacy := collectSnapshot(t, observation, true)
	strictLegacy := collectSnapshot(t, observation, true, true)
	if legacy.scope != "github.com/faustbrian/go-retry/retrytelemetry" || strictLegacy.scope != legacy.scope || !reflect.DeepEqual(strictLegacy, legacy) {
		t.Fatalf("legacy=%+v strict legacy=%+v", legacy, strictLegacy)
	}
}

func TestObserverInstrumentsAreSynchronousConcurrentReentrantAndPanicTransparent(t *testing.T) {
	t.Run("ordered once", func(t *testing.T) {
		var calls []string
		observer := newInstrumentCallbackObserver(t, func(name string) { calls = append(calls, name) })
		observer.Observe(retry.Observation{})
		if !reflect.DeepEqual(calls, []string{"attempts", "elapsed", "delay"}) {
			t.Fatalf("instrument calls = %v", calls)
		}
	})

	t.Run("blocking and concurrent", func(t *testing.T) {
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		observer := newCallbackObserver(t, func(ctx context.Context, _ int64, _ ...metric.AddOption) {
			if ctx != context.Background() {
				t.Error("instrument did not receive context.Background")
			}
			entered <- struct{}{}
			<-release
		})
		done := make(chan struct{}, 2)
		for range 2 {
			go func() { observer.Observe(retry.Observation{}); done <- struct{}{} }()
		}
		for range 2 {
			select {
			case <-entered:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("concurrent instrument did not enter")
			}
		}
		select {
		case <-done:
			close(release)
			t.Fatal("Observe returned while instrument was blocked")
		default:
		}
		close(release)
		for range 2 {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("Observe did not return")
			}
		}
	})

	t.Run("reentrant", func(t *testing.T) {
		var observer *retryotel.Observer
		calls := 0
		observer = newCallbackObserver(t, func(context.Context, int64, ...metric.AddOption) {
			calls++
			if calls == 1 {
				observer.Observe(retry.Observation{Attempt: 2})
			}
		})
		observer.Observe(retry.Observation{Attempt: 1})
		if calls != 2 {
			t.Fatalf("instrument calls = %d", calls)
		}
	})

	for _, sink := range []string{"attempts", "elapsed", "delay"} {
		t.Run("panic "+sink, func(t *testing.T) {
			want := errors.New("instrument panic")
			observer := newInstrumentCallbackObserver(t, func(name string) {
				if name == sink {
					panic(want)
				}
			})
			defer func() {
				//nolint:errorlint // Panic value identity is part of the instrument callback contract.
				if recovered := recover(); recovered != want {
					t.Fatalf("recovered = %v", recovered)
				}
			}()
			observer.Observe(retry.Observation{})
		})
	}
}

func TestDoStrictIsolatesSuccessorOTelInstrumentPanic(t *testing.T) {
	t.Parallel()

	observer := newCallbackObserver(t, func(context.Context, int64, ...metric.AddOption) { panic("instrument panic") })
	policy, err := retry.NewPolicyStrict(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: retry.SystemClock{},
		Sleeper: retry.SystemSleeper{}, Classifier: retry.RetryableClassifier(), Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := retry.DoStrict(context.Background(), policy, func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestDoStrictDeliversReachedOTelObservationAfterCancellationRace(t *testing.T) {
	t.Parallel()

	var calls []string
	observer := newInstrumentCallbackObserver(t, func(name string) { calls = append(calls, name) })
	policy, err := retry.NewPolicyStrict(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 1, Clock: retry.SystemClock{},
		Sleeper: retry.SystemSleeper{}, Classifier: retry.RetryableClassifier(), Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, err := retry.DoStrict(ctx, policy, func(context.Context) (retry.AttemptResult[string], error) {
		cancel()
		return retry.AttemptResult[string]{Value: "done", Outcome: retry.OutcomeKnown}, nil
	})
	if err != nil || result.Value != "done" || !reflect.DeepEqual(calls, []string{"attempts", "elapsed", "delay"}) {
		t.Fatalf("result=%+v err=%v calls=%v", result, err, calls)
	}
}

func TestObserverValidatesPolicyIDAndZeroReceiversAreNoOps(t *testing.T) {
	t.Parallel()

	provider := metricnoop.NewMeterProvider()
	if _, err := retryotel.New(retryotel.Options{MeterProvider: provider, PolicyID: strings.Repeat("x", retryotel.MaxPolicyIDLength)}); err != nil {
		t.Fatalf("maximum policy ID: %v", err)
	}
	if observer, err := retryotel.New(retryotel.Options{MeterProvider: provider, PolicyID: strings.Repeat("x", retryotel.MaxPolicyIDLength+1)}); observer != nil || err == nil || err.Error() != "invalid retry policy: policy ID exceeds 128 bytes" || !errors.Is(err, retry.ErrInvalidPolicy) {
		t.Fatalf("oversized policy ID = (%v,%v)", observer, err)
	}
	for _, observer := range []*retryotel.Observer{{}, nil} {
		observer.Observe(retry.Observation{Reason: retry.ReasonOutcomeUnknown})
	}
}

func TestObserverBoundsEveryClassificationAndReason(t *testing.T) {
	t.Parallel()

	observer, err := retryotel.New(retryotel.Options{MeterProvider: metricnoop.NewMeterProvider()})
	if err != nil {
		t.Fatal(err)
	}
	for _, classification := range []retry.Classification{0, retry.ClassificationPermanent, retry.ClassificationRetryable, 99} {
		observer.Observe(retry.Observation{Classification: classification})
	}
	for _, reason := range []retry.Reason{"", retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted,
		retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget, retry.ReasonAttemptBudget,
		retry.ReasonClassifierFailure, retry.ReasonSleeperFailure, retry.ReasonWorkBudget, retry.ReasonOutcomeUnknown, "hostile"} {
		observer.Observe(retry.Observation{Reason: reason})
	}
}

func TestNewPropagatesEachInstrumentConstructionError(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument failed")
	tests := []*successorErrorMeter{
		{Meter: metricnoop.NewMeterProvider().Meter("test"), counterErr: want},
		{Meter: metricnoop.NewMeterProvider().Meter("test"), histogramErrs: []error{want}},
		{Meter: metricnoop.NewMeterProvider().Meter("test"), histogramErrs: []error{nil, want}},
	}
	for _, meter := range tests {
		provider := successorErrorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: meter}
		if observer, err := retryotel.New(retryotel.Options{MeterProvider: provider}); observer != nil || !errors.Is(err, want) {
			t.Fatalf("New = (%v,%v)", observer, err)
		}
	}
}

func TestNewInvokesProviderAndInstrumentConstructorsSynchronously(t *testing.T) {
	stages := []string{"meter", "attempts", "elapsed", "delay"}
	t.Run("ordered once", func(t *testing.T) {
		var calls []string
		provider := newConstructionProvider(func(stage string) { calls = append(calls, stage) })
		if observer, err := retryotel.New(retryotel.Options{MeterProvider: provider}); observer == nil || err != nil {
			t.Fatalf("New = (%v,%v)", observer, err)
		}
		if !reflect.DeepEqual(calls, stages) {
			t.Fatalf("constructor calls = %v", calls)
		}
	})

	for _, stage := range stages {
		t.Run(stage+" blocking concurrent", func(t *testing.T) {
			entered := make(chan struct{}, 2)
			release := make(chan struct{})
			provider := newConstructionProvider(func(got string) {
				if got == stage {
					entered <- struct{}{}
					<-release
				}
			})
			done := make(chan struct{}, 2)
			for range 2 {
				go func() {
					_, _ = retryotel.New(retryotel.Options{MeterProvider: provider})
					done <- struct{}{}
				}()
			}
			for range 2 {
				select {
				case <-entered:
				case <-time.After(time.Second):
					close(release)
					t.Fatal("constructors did not enter concurrently")
				}
			}
			select {
			case <-done:
				close(release)
				t.Fatal("New returned while constructor blocked")
			default:
			}
			close(release)
			for range 2 {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("New did not return after constructor release")
				}
			}
		})

		t.Run(stage+" reentrant", func(t *testing.T) {
			var provider *constructionProvider
			reentered := false
			provider = newConstructionProvider(func(got string) {
				if got != stage || reentered {
					return
				}
				reentered = true
				if observer, err := retryotel.New(retryotel.Options{MeterProvider: provider}); observer == nil || err != nil {
					t.Fatalf("reentrant New = (%v,%v)", observer, err)
				}
			})
			if observer, err := retryotel.New(retryotel.Options{MeterProvider: provider}); observer == nil || err != nil || !reentered {
				t.Fatalf("New = (%v,%v), reentered=%v", observer, err, reentered)
			}
		})

		t.Run(stage+" panic", func(t *testing.T) {
			want := errors.New("constructor panic")
			provider := newConstructionProvider(func(got string) {
				if got == stage {
					panic(want)
				}
			})
			defer func() {
				//nolint:errorlint // Panic value identity is part of the provider contract.
				if recovered := recover(); recovered != want {
					t.Fatalf("recovered = %v", recovered)
				}
			}()
			_, _ = retryotel.New(retryotel.Options{MeterProvider: provider})
		})
	}
}

func TestSuccessorOTelTypesOwnReflectionIdentity(t *testing.T) {
	t.Parallel()

	const want = "github.com/faustbrian/go-retry/adapters/otel"
	for _, value := range []any{retryotel.Options{}, retryotel.Observer{}} {
		if got := reflect.TypeOf(value).PkgPath(); got != want {
			t.Fatalf("%T PkgPath = %q", value, got)
		}
	}
}

type nilMeterProvider struct{ metric.MeterProvider }

func (*nilMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	panic("Meter must not be called")
}

type successorErrorMeterProvider struct {
	metric.MeterProvider
	meter *successorErrorMeter
}

type constructionProvider struct {
	metric.MeterProvider
	meter metric.Meter
	hook  func(string)
}

func newConstructionProvider(hook func(string)) *constructionProvider {
	base := metricnoop.NewMeterProvider()
	return &constructionProvider{MeterProvider: base, meter: base.Meter("construction"), hook: hook}
}

func (provider *constructionProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	provider.hook("meter")
	return constructionMeter{Meter: provider.meter, hook: provider.hook}
}

type constructionMeter struct {
	metric.Meter
	hook func(string)
}

func (meter constructionMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	meter.hook("attempts")
	return meter.Meter.Int64Counter(name, options...)
}

func (meter constructionMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	switch name {
	case "retry.elapsed":
		meter.hook("elapsed")
	case "retry.delay":
		meter.hook("delay")
	}
	return meter.Meter.Float64Histogram(name, options...)
}

func (provider successorErrorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type successorErrorMeter struct {
	metric.Meter
	counterErr    error
	histogramErrs []error
	histogramCall int
}

type metricSnapshot struct {
	scope      string
	units      map[string]string
	values     map[string]float64
	attributes map[string]map[string]string
}

func collectSnapshot(t *testing.T, observation retry.Observation, compatibility bool, strictCompatibility ...bool) metricSnapshot {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	if compatibility {
		var observer *retrytelemetry.Observer
		var err error
		if len(strictCompatibility) > 0 && strictCompatibility[0] {
			observer, err = retrytelemetry.NewStrict(retrytelemetry.Options{MeterProvider: provider, PolicyID: "policy"})
		} else {
			observer, err = legacyObserver(provider)
		}
		if err != nil {
			t.Fatal(err)
		}
		observer.Observe(observation)
	} else {
		observer, err := retryotel.New(retryotel.Options{MeterProvider: provider, PolicyID: "policy"})
		if err != nil {
			t.Fatal(err)
		}
		observer.Observe(observation)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	if len(collected.ScopeMetrics) != 1 {
		t.Fatalf("scope metrics = %d", len(collected.ScopeMetrics))
	}
	snapshot := metricSnapshot{
		scope: collected.ScopeMetrics[0].Scope.Name, units: map[string]string{},
		values: map[string]float64{}, attributes: map[string]map[string]string{},
	}
	for _, measured := range collected.ScopeMetrics[0].Metrics {
		snapshot.units[measured.Name] = measured.Unit
		measuredAttributes := map[string]string{}
		switch data := measured.Data.(type) {
		case metricdata.Sum[int64]:
			if len(data.DataPoints) != 1 {
				t.Fatalf("%s points = %d", measured.Name, len(data.DataPoints))
			}
			snapshot.values[measured.Name] = float64(data.DataPoints[0].Value)
			copyAttributes(measuredAttributes, data.DataPoints[0].Attributes.ToSlice())
		case metricdata.Histogram[float64]:
			if len(data.DataPoints) != 1 || data.DataPoints[0].Count != 1 {
				t.Fatalf("%s histogram = %+v", measured.Name, data.DataPoints)
			}
			snapshot.values[measured.Name] = data.DataPoints[0].Sum
			copyAttributes(measuredAttributes, data.DataPoints[0].Attributes.ToSlice())
		default:
			t.Fatalf("%s data = %T", measured.Name, measured.Data)
		}
		snapshot.attributes[measured.Name] = measuredAttributes
	}
	wantAttributes := map[string]string{"retry.policy.id": "policy", "retry.classification": classificationName(observation.Classification), "retry.reason": reasonName(observation.Reason)}
	if !reflect.DeepEqual(snapshot.units, map[string]string{"retry.attempts": "{attempt}", "retry.elapsed": "s", "retry.delay": "s"}) || !reflect.DeepEqual(snapshot.values, map[string]float64{"retry.attempts": 1, "retry.elapsed": 3, "retry.delay": 1}) || !reflect.DeepEqual(snapshot.attributes, map[string]map[string]string{"retry.attempts": wantAttributes, "retry.elapsed": wantAttributes, "retry.delay": wantAttributes}) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	return snapshot
}

func copyAttributes(target map[string]string, values []attribute.KeyValue) {
	for _, value := range values {
		target[string(value.Key)] = value.Value.AsString()
	}
}

func equalMetricContent(left, right metricSnapshot) bool {
	return left.scope == "github.com/faustbrian/go-retry/adapters/otel" && right.scope == "github.com/faustbrian/go-retry/retrytelemetry" && reflect.DeepEqual(left.units, right.units) && reflect.DeepEqual(left.values, right.values) && reflect.DeepEqual(left.attributes, right.attributes)
}

func classificationName(value retry.Classification) string {
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

func reasonName(value retry.Reason) string {
	switch value {
	case "":
		return "none"
	case retry.ReasonSucceeded, retry.ReasonPermanent, retry.ReasonAttemptsExhausted, retry.ReasonCanceled, retry.ReasonElapsedBudget, retry.ReasonSleepBudget, retry.ReasonAttemptBudget, retry.ReasonClassifierFailure, retry.ReasonSleeperFailure, retry.ReasonWorkBudget, retry.ReasonOutcomeUnknown:
		return string(value)
	default:
		return "unknown"
	}
}

func legacyObserver(provider metric.MeterProvider) (*retrytelemetry.Observer, error) {
	return retrytelemetry.New(retrytelemetry.Options{MeterProvider: provider, PolicyID: "policy"})
}

type callbackCounter struct {
	metric.Int64Counter
	add func(context.Context, int64, ...metric.AddOption)
}

type callbackHistogram struct {
	metric.Float64Histogram
	record func(context.Context, float64, ...metric.RecordOption)
}

func (histogram callbackHistogram) Record(ctx context.Context, value float64, options ...metric.RecordOption) {
	histogram.record(ctx, value, options...)
}

func (counter callbackCounter) Add(ctx context.Context, value int64, options ...metric.AddOption) {
	counter.add(ctx, value, options...)
}

type callbackMeter struct {
	metric.Meter
	counter    metric.Int64Counter
	histograms map[string]metric.Float64Histogram
}

func (meter callbackMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return meter.counter, nil
}

func (meter callbackMeter) Float64Histogram(name string, _ ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if histogram := meter.histograms[name]; histogram != nil {
		return histogram, nil
	}
	return meter.Meter.Float64Histogram(name)
}

type callbackProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider callbackProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

func newCallbackObserver(t *testing.T, add func(context.Context, int64, ...metric.AddOption)) *retryotel.Observer {
	t.Helper()
	baseMeter := metricnoop.NewMeterProvider().Meter("test")
	provider := callbackProvider{
		MeterProvider: metricnoop.NewMeterProvider(),
		meter: callbackMeter{Meter: baseMeter, counter: callbackCounter{
			Int64Counter: mustCounter(t, baseMeter), add: add,
		}},
	}
	observer, err := retryotel.New(retryotel.Options{MeterProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	return observer
}

func newInstrumentCallbackObserver(t *testing.T, callback func(string)) *retryotel.Observer {
	t.Helper()
	baseMeter := metricnoop.NewMeterProvider().Meter("test")
	provider := callbackProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: callbackMeter{
		Meter: baseMeter,
		counter: callbackCounter{Int64Counter: mustCounter(t, baseMeter), add: func(ctx context.Context, _ int64, _ ...metric.AddOption) {
			if ctx != context.Background() {
				t.Error("counter context is not context.Background")
			}
			callback("attempts")
		}},
		histograms: map[string]metric.Float64Histogram{
			"retry.elapsed": callbackHistogram{Float64Histogram: mustHistogram(t, baseMeter, "elapsed"), record: func(ctx context.Context, _ float64, _ ...metric.RecordOption) {
				if ctx != context.Background() {
					t.Error("elapsed context is not context.Background")
				}
				callback("elapsed")
			}},
			"retry.delay": callbackHistogram{Float64Histogram: mustHistogram(t, baseMeter, "delay"), record: func(ctx context.Context, _ float64, _ ...metric.RecordOption) {
				if ctx != context.Background() {
					t.Error("delay context is not context.Background")
				}
				callback("delay")
			}},
		},
	}}
	observer, err := retryotel.New(retryotel.Options{MeterProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	return observer
}

func mustCounter(t *testing.T, meter metric.Meter) metric.Int64Counter {
	t.Helper()
	counter, err := meter.Int64Counter("callback")
	if err != nil {
		t.Fatal(err)
	}
	return counter
}

func mustHistogram(t *testing.T, meter metric.Meter, name string) metric.Float64Histogram {
	t.Helper()
	histogram, err := meter.Float64Histogram(name)
	if err != nil {
		t.Fatal(err)
	}
	return histogram
}

func (meter *successorErrorMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return meter.Meter.Int64Counter(name, options...)
}

func (meter *successorErrorMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if meter.histogramCall < len(meter.histogramErrs) {
		err := meter.histogramErrs[meter.histogramCall]
		meter.histogramCall++
		if err != nil {
			return nil, err
		}
	}
	return meter.Meter.Float64Histogram(name, options...)
}
