// Package retrypgx classifies PostgreSQL errors by SQLSTATE. It does not
// decide whether a transaction or statement is safe to repeat.
package retrypgx

import (
	"context"
	"errors"
	"io"
	"net"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/jackc/pgx/v5/pgconn"
)

// Classifier conservatively classifies PostgreSQL failures.
type Classifier struct{}

// NewClassifier constructs an immutable PostgreSQL classifier.
func NewClassifier() Classifier { return Classifier{} }

// Classify implements retry.Classifier. Serialization, deadlock, lock
// availability, server restart, and connection-class failures are transient.
func (Classifier) Classify(ctx context.Context, err error) (retry.Classification, error) {
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
	if ctx != nil && ctx.Err() != nil &&
		errors.Is(err, ctx.Err()) {
		return retry.ClassificationPermanent, nil
	}
	if pgconn.SafeToRetry(err) ||
		pgconn.Timeout(err) ||
		errors.Is(err, pgconn.ErrConnClosed) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return retry.ClassificationRetryable, nil
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return retry.ClassificationPermanent, nil
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return retry.ClassificationRetryable, nil
	}

	return retry.ClassificationPermanent, nil
}

var _ retry.Classifier = Classifier{}
