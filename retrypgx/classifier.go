// Package retrypgx classifies PostgreSQL errors by SQLSTATE. It does not
// decide whether a transaction or statement is safe to repeat.
package retrypgx

import (
	"context"
	"errors"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/jackc/pgx/v5/pgconn"
)

// Classifier conservatively classifies PostgreSQL failures.
type Classifier struct{}

// NewClassifier constructs an immutable PostgreSQL classifier.
func NewClassifier() Classifier { return Classifier{} }

// Classify implements retry.Classifier. Serialization, deadlock, lock
// availability, server restart, and connection-class failures are transient.
func (Classifier) Classify(_ context.Context, err error) (retry.Classification, error) {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return retry.ClassificationPermanent, nil
	}
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

var _ retry.Classifier = Classifier{}
