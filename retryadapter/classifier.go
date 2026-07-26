// Package retryadapter provides explicit classifier seams for integrations
// whose transient failures are domain-specific. It never decides idempotency.
package retryadapter

import (
	"context"
	"fmt"

	retry "github.com/faustbrian/golib/pkg/retry"
)

type classifier struct {
	transient func(error) bool
}

func (classifier classifier) Classify(_ context.Context, err error) (retry.Classification, error) {
	if classifier.transient(err) {
		return retry.ClassificationRetryable, nil
	}
	return retry.ClassificationPermanent, nil
}

// Queue constructs a classifier using queue-backend evidence supplied by the
// caller. Message acknowledgement and delivery safety remain caller-owned.
func Queue(transient func(error) bool) (retry.Classifier, error) {
	return newClassifier("queue", transient)
}

// Webhook constructs a classifier using webhook transport evidence supplied
// by the caller. Delivery idempotency remains caller-owned.
func Webhook(transient func(error) bool) (retry.Classifier, error) {
	return newClassifier("webhook", transient)
}

// Filesystem constructs a classifier using filesystem/backend evidence
// supplied by the caller. Mutation replay safety remains caller-owned.
func Filesystem(transient func(error) bool) (retry.Classifier, error) {
	return newClassifier("filesystem", transient)
}

// ObjectStorage constructs a classifier using provider evidence supplied by
// the caller. Multipart and object mutation safety remain caller-owned.
func ObjectStorage(transient func(error) bool) (retry.Classifier, error) {
	return newClassifier("object storage", transient)
}

func newClassifier(kind string, transient func(error) bool) (retry.Classifier, error) {
	if transient == nil {
		return nil, fmt.Errorf("%w: %s transient predicate is required", retry.ErrInvalidPolicy, kind)
	}
	return classifier{transient: transient}, nil
}

var _ retry.Classifier = classifier{}
