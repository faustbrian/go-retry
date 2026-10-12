// Package retrypgx classifies PostgreSQL errors by SQLSTATE. It does not
// decide whether a transaction or statement is safe to repeat. NewClassifier
// or the zero Classifier is ready and stateless. It imports pgx error helpers
// but opens no pool or connection. Unlike the canonical adapter, SQLSTATE
// classification comes before caller cancellation and nil context is accepted.
// Context/error traversal is synchronous; custom implementations are trusted.
//
// Deprecated: use github.com/faustbrian/go-retry/v2/adapters/postgres. This
// package remains supported through the documented compatibility interval.
package retrypgx

import (
	"context"
	"errors"
	"io"
	"net"

	retry "github.com/faustbrian/go-retry/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

// Classifier conservatively classifies PostgreSQL failures.
type Classifier struct{}

// NewClassifier constructs an immutable PostgreSQL classifier.
func NewClassifier() Classifier { return Classifier{} }

// Classify implements retry.Classifier. Serialization, deadlock, lock
// availability, server restart, and connection-class failures are transient.
// PgError takes precedence over caller cancellation. After that, a returned
// error matching ctx.Err is permanent; pgx/EOF connection evidence precedes
// remaining returned context sentinels. This ordering is retained compatibility,
// not proof that a failed transaction can be safely replayed.
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
	switch ctx {
	case nil:
	default:
		switch ctxErr := ctx.Err(); ctxErr {
		case nil:
		default:
			if errors.Is(err, ctxErr) {
				return retry.ClassificationPermanent, nil
			}
		}
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
