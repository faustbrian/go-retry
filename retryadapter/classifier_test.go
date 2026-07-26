package retryadapter_test

import (
	"context"
	"errors"
	"testing"

	retry "github.com/faustbrian/golib/pkg/retry"
	"github.com/faustbrian/golib/pkg/retry/retryadapter"
)

func TestDomainAdaptersRequireCallerOwnedTransientDecision(t *testing.T) {
	t.Parallel()

	temporary := errors.New("temporary")
	permanent := errors.New("permanent")
	factories := []struct {
		name string
		new  func(func(error) bool) (retry.Classifier, error)
	}{
		{"queue", retryadapter.Queue},
		{"webhook", retryadapter.Webhook},
		{"filesystem", retryadapter.Filesystem},
		{"object storage", retryadapter.ObjectStorage},
	}

	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			if _, err := factory.new(nil); !errors.Is(err, retry.ErrInvalidPolicy) {
				t.Fatalf("nil predicate error = %v, want ErrInvalidPolicy", err)
			}
			classifier, err := factory.new(func(err error) bool { return errors.Is(err, temporary) })
			if err != nil {
				t.Fatalf("construct classifier: %v", err)
			}
			classification, err := classifier.Classify(context.Background(), temporary)
			if err != nil || classification != retry.ClassificationRetryable {
				t.Fatalf("temporary = (%v, %v), want retryable", classification, err)
			}
			classification, err = classifier.Classify(context.Background(), permanent)
			if err != nil || classification != retry.ClassificationPermanent {
				t.Fatalf("permanent = (%v, %v), want permanent", classification, err)
			}
		})
	}
}
