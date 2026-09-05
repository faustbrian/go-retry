package retrypostgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"

	retry "github.com/faustbrian/go-retry"
	"github.com/jackc/pgx/v5/pgconn"
)

// Classifier conservatively classifies PostgreSQL failures. It owns no
// resources and its zero value is ready for concurrent use.
type Classifier struct{}

// New constructs an immutable PostgreSQL classifier.
func New() Classifier { return Classifier{} }

// Classify implements retry.Classifier. Caller cancellation and deadline
// always take precedence over otherwise transient backend failures.
func (Classifier) Classify(ctx context.Context, err error) (retry.Classification, error) {
	if nilContext(ctx) {
		return 0, fmt.Errorf("%w: context is required", retry.ErrInvalidPolicy)
	}
	if ctx.Err() != nil {
		//nolint:nilerr // Active caller cancellation deliberately overrides the classified error.
		return retry.ClassificationPermanent, nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		if len(postgresError.Code) == 5 && postgresError.Code[:2] == "08" {
			return retry.ClassificationRetryable, nil
		}
		switch postgresError.Code {
		case "40001", "40P01", "55P03", "57P01", "57P02", "57P03":
			return retry.ClassificationRetryable, nil
		default:
			return retry.ClassificationPermanent, nil
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return retry.ClassificationPermanent, nil
	}
	if pgconn.SafeToRetry(err) ||
		pgconn.Timeout(err) ||
		errors.Is(err, pgconn.ErrConnClosed) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return retry.ClassificationRetryable, nil
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return retry.ClassificationRetryable, nil
	}
	return retry.ClassificationPermanent, nil
}

func nilContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	value := reflect.ValueOf(ctx)
	//nolint:exhaustive // Only kinds that can contain nil require a case.
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ retry.Classifier = Classifier{}
