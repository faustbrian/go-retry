package retrypostgres_test

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"syscall"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry"
	retrypostgres "github.com/faustbrian/go-retry/adapters/postgres"

	//lint:ignore SA1019 Legacy parity is the compatibility contract under test.
	legacy "github.com/faustbrian/go-retry/retrypgx" //nolint:staticcheck // Legacy parity is the compatibility contract under test.
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifierPreservesPostgresAndConnectionParity(t *testing.T) {
	t.Parallel()

	classifier := retrypostgres.New()
	legacyClassifier := legacy.NewClassifier()
	inputs := []error{
		&pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40P01"},
		&pgconn.PgError{Code: "55P03"}, &pgconn.PgError{Code: "57P01"},
		&pgconn.PgError{Code: "08006"}, &pgconn.PgError{Code: "23505"},
		postgresSafeRetryFailure{}, errors.Join(errors.New("query"), pgconn.ErrConnClosed),
		io.EOF, io.ErrUnexpectedEOF,
		&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET},
		errors.New("unknown"), nil,
	}
	for _, input := range inputs {
		got, err := classifier.Classify(context.Background(), input)
		legacyGot, legacyErr := legacyClassifier.Classify(context.Background(), input)
		if err != nil || legacyErr != nil || got != legacyGot {
			t.Fatalf("Classify(%T) = (%v,%v), legacy=(%v,%v)", input, got, err, legacyGot, legacyErr)
		}
	}
}

func TestClassifierCallerContextAndContextErrorsTakePrecedence(t *testing.T) {
	t.Parallel()

	classifier := retrypostgres.New()
	contexts := []context.Context{canceledPostgresContext(), expiredPostgresContext()}
	inputs := []error{
		&pgconn.PgError{Code: "40001"}, postgresSafeRetryFailure{}, io.EOF,
		&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET},
	}
	for _, ctx := range contexts {
		for _, input := range inputs {
			classification, err := classifier.Classify(ctx, input)
			if err != nil || classification != retry.ClassificationPermanent {
				t.Fatalf("Classify(%v,%T) = (%v,%v), want permanent", ctx.Err(), input, classification, err)
			}
		}
	}
	for _, input := range []error{
		postgresSafeContextFailure{cause: context.Canceled},
		postgresSafeContextFailure{cause: context.DeadlineExceeded},
		errors.Join(postgresSafeRetryFailure{}, context.Canceled),
	} {
		classification, err := classifier.Classify(context.Background(), input)
		if err != nil || classification != retry.ClassificationPermanent {
			t.Fatalf("Classify(%T) = (%v,%v), want permanent", input, classification, err)
		}
	}
}

func TestClassifierReturnedContextErrorPreservesSQLStatePrecedence(t *testing.T) {
	t.Parallel()

	classifier := retrypostgres.New()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		input := errors.Join(&pgconn.PgError{Code: "40001"}, sentinel)
		classification, err := classifier.Classify(context.Background(), input)
		if err != nil || classification != retry.ClassificationRetryable {
			t.Fatalf("Classify(joined SQLSTATE,%v) = (%v,%v), want retryable", sentinel, classification, err)
		}
	}
}

func TestClassifierReturnedContextErrorPrecedesConnectionEvidence(t *testing.T) {
	t.Parallel()

	classifier := retrypostgres.New()
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		inputs := []error{
			errors.Join(postgresSafeRetryFailure{}, sentinel),
			errors.Join(postgresTimeoutFailure{}, sentinel),
			errors.Join(pgconn.ErrConnClosed, sentinel),
			errors.Join(io.EOF, sentinel),
			errors.Join(io.ErrUnexpectedEOF, sentinel),
			errors.Join(&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, sentinel),
		}
		for _, input := range inputs {
			classification, err := classifier.Classify(context.Background(), input)
			if err != nil || classification != retry.ClassificationPermanent {
				t.Fatalf("Classify(%T joined with %v) = (%v,%v), want permanent", input, sentinel, classification, err)
			}
		}
	}
}

func TestLegacyClassifierKeepsPgErrorFirstPrecedence(t *testing.T) {
	t.Parallel()

	ctx := canceledPostgresContext()
	failure := &pgconn.PgError{Code: "40001"}
	legacyClassification, legacyErr := legacy.NewClassifier().Classify(ctx, failure)
	strictClassification, strictErr := retrypostgres.New().Classify(ctx, failure)
	if legacyErr != nil || legacyClassification != retry.ClassificationRetryable {
		t.Fatalf("legacy = (%v,%v)", legacyClassification, legacyErr)
	}
	if strictErr != nil || strictClassification != retry.ClassificationPermanent {
		t.Fatalf("successor = (%v,%v)", strictClassification, strictErr)
	}
}

func TestClassifierRejectsNilAndTypedNilContext(t *testing.T) {
	t.Parallel()

	classifier := retrypostgres.New()
	var typedNil *postgresNilContext
	for _, ctx := range []context.Context{nil, typedNil} {
		classification, err := classifier.Classify(ctx, &pgconn.PgError{Code: "40001"})
		if classification != 0 || err == nil || err.Error() != "invalid retry policy: context is required" || !errors.Is(err, retry.ErrInvalidPolicy) {
			t.Fatalf("Classify = (%v,%v)", classification, err)
		}
	}
}

func TestClassifierOwnsSuccessorIdentityAndZeroValueWorks(t *testing.T) {
	t.Parallel()

	if got := reflect.TypeOf(retrypostgres.Classifier{}).PkgPath(); got != "github.com/faustbrian/go-retry/adapters/postgres" {
		t.Fatalf("PkgPath = %q", got)
	}
	classification, err := (retrypostgres.Classifier{}).Classify(context.Background(), &pgconn.PgError{Code: "40001"})
	if err != nil || classification != retry.ClassificationRetryable {
		t.Fatalf("zero Classifier = (%v,%v)", classification, err)
	}
}

type postgresSafeRetryFailure struct{}

func (postgresSafeRetryFailure) Error() string     { return "safe" }
func (postgresSafeRetryFailure) SafeToRetry() bool { return true }

type postgresTimeoutFailure struct{}

func (postgresTimeoutFailure) Error() string   { return "timeout" }
func (postgresTimeoutFailure) Timeout() bool   { return true }
func (postgresTimeoutFailure) Temporary() bool { return true }

type postgresSafeContextFailure struct{ cause error }

func (failure postgresSafeContextFailure) Error() string { return "safe context" }
func (failure postgresSafeContextFailure) Unwrap() error { return failure.cause }
func (postgresSafeContextFailure) SafeToRetry() bool     { return true }

type postgresNilContext struct{}

func (*postgresNilContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*postgresNilContext) Done() <-chan struct{}       { return nil }
func (*postgresNilContext) Err() error                  { return nil }
func (*postgresNilContext) Value(any) any               { return nil }

func canceledPostgresContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func expiredPostgresContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	return ctx
}
