package retrypgx_test

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/faustbrian/golib/pkg/retry/retrypgx"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifierRetriesOnlySafelyTransientSQLStates(t *testing.T) {
	t.Parallel()

	classifier := retrypgx.NewClassifier()
	for _, code := range []string{"40001", "40P01", "55P03", "57P01", "57P02", "57P03", "08000", "08006"} {
		classification, err := classifier.Classify(context.Background(), &pgconn.PgError{Code: code})
		if err != nil || classification != retry.ClassificationRetryable {
			t.Errorf("SQLSTATE %s = (%v, %v), want retryable", code, classification, err)
		}
	}
	for _, code := range []string{"23505", "23503", "42601", "57014", "28P01"} {
		classification, err := classifier.Classify(context.Background(), &pgconn.PgError{Code: code})
		if err != nil || classification != retry.ClassificationPermanent {
			t.Errorf("SQLSTATE %s = (%v, %v), want permanent", code, classification, err)
		}
	}
}

func TestClassifierHandlesWrappedAndUnknownErrors(t *testing.T) {
	t.Parallel()

	classifier := retrypgx.NewClassifier()
	classification, err := classifier.Classify(context.Background(), errors.Join(errors.New("query"), &pgconn.PgError{Code: "40001"}))
	if err != nil || classification != retry.ClassificationRetryable {
		t.Fatalf("wrapped error = (%v, %v), want retryable", classification, err)
	}
	classification, err = classifier.Classify(context.Background(), errors.New("not PostgreSQL"))
	if err != nil || classification != retry.ClassificationPermanent {
		t.Fatalf("unknown error = (%v, %v), want permanent", classification, err)
	}
}

func TestClassifierRetriesTransientConnectionFailures(t *testing.T) {
	t.Parallel()

	classifier := retrypgx.NewClassifier()
	failures := []error{
		safeRetryFailure{},
		safeContextFailure{cause: context.DeadlineExceeded},
		errors.Join(errors.New("query"), pgconn.ErrConnClosed),
		io.EOF,
		io.ErrUnexpectedEOF,
		&net.OpError{
			Op:  "read",
			Net: "tcp",
			Err: syscall.ECONNRESET,
		},
	}
	for _, failure := range failures {
		classification, err := classifier.Classify(
			context.Background(),
			failure,
		)
		if err != nil ||
			classification != retry.ClassificationRetryable {
			t.Errorf(
				"failure %T = (%v, %v), want retryable",
				failure,
				classification,
				err,
			)
		}
	}
	for _, failure := range []error{
		context.Canceled,
		context.DeadlineExceeded,
		errors.New("unknown"),
	} {
		classification, err := classifier.Classify(
			context.Background(),
			failure,
		)
		if err != nil ||
			classification != retry.ClassificationPermanent {
			t.Errorf(
				"failure %T = (%v, %v), want permanent",
				failure,
				classification,
				err,
			)
		}
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	classification, err := classifier.Classify(
		canceled,
		safeContextFailure{cause: context.Canceled},
	)
	if err != nil || classification != retry.ClassificationPermanent {
		t.Fatalf(
			"caller cancellation = (%v, %v), want permanent",
			classification,
			err,
		)
	}
}

type safeRetryFailure struct{}

func (safeRetryFailure) Error() string { return "safe to retry" }

func (safeRetryFailure) SafeToRetry() bool { return true }

type safeContextFailure struct {
	cause error
}

func (failure safeContextFailure) Error() string {
	return "safe context failure"
}

func (failure safeContextFailure) Unwrap() error {
	return failure.cause
}

func (safeContextFailure) SafeToRetry() bool { return true }
