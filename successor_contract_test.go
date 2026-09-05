package retry_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
	retryhttp "github.com/faustbrian/go-retry/adapters/http"
	retryotel "github.com/faustbrian/go-retry/adapters/otel"
	retrypostgres "github.com/faustbrian/go-retry/adapters/postgres"
	retryslog "github.com/faustbrian/go-retry/adapters/slog"
	"github.com/faustbrian/go-retry/retryadapter"

	//lint:ignore SA1019 Legacy compatibility and distinct identity are under test.
	legacyhttp "github.com/faustbrian/go-retry/retryhttp" //nolint:staticcheck // Legacy compatibility and distinct identity are under test.
	//lint:ignore SA1019 Legacy compatibility and distinct identity are under test.
	legacylog "github.com/faustbrian/go-retry/retrylog" //nolint:staticcheck // Legacy compatibility and distinct identity are under test.
	//lint:ignore SA1019 Legacy compatibility and distinct identity are under test.
	legacypgx "github.com/faustbrian/go-retry/retrypgx" //nolint:staticcheck // Legacy compatibility and distinct identity are under test.
	//lint:ignore SA1019 Legacy compatibility and distinct identity are under test.
	legacyotel "github.com/faustbrian/go-retry/retrytelemetry" //nolint:staticcheck // Legacy compatibility and distinct identity are under test.
)

func TestSuccessorExportedContractsCompileIndependently(t *testing.T) {
	t.Parallel()

	_ = (func(retry.Config) (*retry.Policy, error))(retry.NewPolicyStrict)
	var _ retry.StrictOperation[string] = func(context.Context) (retry.AttemptResult[string], error) {
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, nil
	}
	_ = (func(context.Context, *retry.Policy, retry.StrictOperation[string]) (retry.StrictResult[string], error))(retry.DoStrict[string])

	_ = (func(retryhttp.Options) (*retryhttp.Classifier, error))(retryhttp.New)
	_ = (func(int, http.Header, error) (*retryhttp.Error, error))(retryhttp.NewError)
	_ = (func(string, time.Time) (time.Duration, bool))(retryhttp.ParseRetryAfter)
	var _ retry.Classifier = (*retryhttp.Classifier)(nil)
	var _ retry.DelayHint = (*retryhttp.Error)(nil)

	_ = (func() retrypostgres.Classifier)(retrypostgres.New)
	var _ retry.Classifier = retrypostgres.Classifier{}
	var _ retry.Observer = (*retryslog.Observer)(nil)
	var _ retry.Observer = (*retryotel.Observer)(nil)

	_ = retryadapter.Queue
	_ = legacyhttp.NewClassifier
	_ = legacylog.New
	_ = legacypgx.NewClassifier
	_ = legacyotel.New
	_ = legacyotel.NewStrict
}

func TestSuccessorNamedTypesHaveDistinctPackageIdentity(t *testing.T) {
	t.Parallel()

	pairs := []struct {
		legacy    any
		successor any
	}{
		{legacyhttp.Options{}, retryhttp.Options{}},
		{legacyhttp.Classifier{}, retryhttp.Classifier{}},
		{legacyhttp.Error{}, retryhttp.Error{}},
		{legacylog.Options{}, retryslog.Options{}},
		{legacylog.Observer{}, retryslog.Observer{}},
		{legacypgx.Classifier{}, retrypostgres.Classifier{}},
		{legacyotel.Options{}, retryotel.Options{}},
		{legacyotel.Observer{}, retryotel.Observer{}},
	}
	for _, pair := range pairs {
		legacyType := reflect.TypeOf(pair.legacy)
		successorType := reflect.TypeOf(pair.successor)
		if legacyType.PkgPath() == successorType.PkgPath() || legacyType == successorType {
			t.Fatalf("legacy %v and successor %v share identity", legacyType, successorType)
		}
	}
}

func TestStrictOutcomeZeroValuesMeanNotDispatched(t *testing.T) {
	t.Parallel()

	var outcome retry.Outcome
	var attempt retry.AttemptResult[string]
	var result retry.StrictResult[string]
	if outcome != retry.OutcomeNotDispatched || attempt.Outcome != retry.OutcomeNotDispatched || attempt.Value != "" || result.Outcome != retry.OutcomeNotDispatched || result.Value != "" || result.Retry.Attempts != 0 || result.Retry.Reason != "" || len(result.Retry.History) != 0 {
		t.Fatalf("outcome=%d attempt=%+v result=%+v", outcome, attempt, result)
	}
}
