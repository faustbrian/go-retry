package retry_test

import (
	"errors"
	"testing"
	"time"

	retry "github.com/faustbrian/go-retry/v3"
)

func TestCanonicalPolicyRejectsTypedNilOptionalCollaborators(t *testing.T) {
	var random *nilRandom
	var observer *nilObserver
	for _, name := range []string{"random", "observer"} {
		t.Run(name, func(t *testing.T) {
			clock := newManualClock(time.Unix(100, 0))
			config := retry.Config{
				Backoff: retry.Constant(0), MaxAttempts: 1, Clock: clock,
				Sleeper: advancingSleeper{clock}, Classifier: retry.RetryableClassifier(),
			}
			if name == "random" {
				config.Random = random
			} else {
				config.Observer = observer
			}
			policy, err := retry.NewPolicy(config)
			if policy != nil || !errors.Is(err, retry.ErrInvalidPolicy) {
				t.Fatalf("canonical policy accepted explicitly typed-nil %s: policy=%v err=%v", name, policy, err)
			}
		})
	}
}
