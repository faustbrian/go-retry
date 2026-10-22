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

	retry "github.com/faustbrian/go-retry/v3"
	retrypostgres "github.com/faustbrian/go-retry/v3/adapters/postgres"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifierPostgresAndConnectionDecisions(t *testing.T) {
	classifier := retrypostgres.New()
	for _, test := range []struct {
		err  error
		want retry.Classification
	}{
		{&pgconn.PgError{Code: "40001"}, retry.ClassificationRetryable}, {&pgconn.PgError{Code: "40P01"}, retry.ClassificationRetryable},
		{&pgconn.PgError{Code: "55P03"}, retry.ClassificationRetryable}, {&pgconn.PgError{Code: "57P01"}, retry.ClassificationRetryable},
		{&pgconn.PgError{Code: "08006"}, retry.ClassificationRetryable}, {&pgconn.PgError{Code: "23505"}, retry.ClassificationPermanent},
		{postgresSafeRetryFailure{}, retry.ClassificationRetryable}, {errors.Join(errors.New("query"), pgconn.ErrConnClosed), retry.ClassificationRetryable},
		{io.EOF, retry.ClassificationRetryable}, {io.ErrUnexpectedEOF, retry.ClassificationRetryable},
		{&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, retry.ClassificationRetryable},
		{errors.New("unknown"), retry.ClassificationPermanent}, {nil, retry.ClassificationPermanent},
	} {
		got, err := classifier.Classify(t.Context(), test.err)
		if err != nil || got != test.want {
			t.Fatalf("classify %T=(%v,%v),want %v", test.err, got, err, test.want)
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

	if got := reflect.TypeOf(retrypostgres.Classifier{}).PkgPath(); got != "github.com/faustbrian/go-retry/v3/adapters/postgres" {
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
