package retry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// ErrInvalidPolicy identifies contradictory, implicit, or unbounded policies.
var ErrInvalidPolicy = errors.New("invalid retry policy")

// MaxHistoryEntries is the largest failure history retained by a policy.
const MaxHistoryEntries = 1024

// Clock supplies policy time. Implementations may additionally implement
// TimeoutClock to make per-attempt timeouts deterministic.
type Clock interface {
	Now() time.Time
}

// TimeoutClock derives deadline contexts using an injected clock.
type TimeoutClock interface {
	Clock
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// Sleeper waits without owning policy or retry decisions.
type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

// Classification is an explicit decision about one operation failure.
type Classification uint8

const (
	// ClassificationPermanent stops execution and preserves the operation error.
	ClassificationPermanent Classification = iota + 1
	// ClassificationRetryable permits another bounded attempt.
	ClassificationRetryable
)

// Classifier classifies an operation failure. Returning an error means the
// classifier itself failed and execution stops.
type Classifier interface {
	Classify(context.Context, error) (Classification, error)
}

// ClassifyFunc adapts a function to Classifier.
type ClassifyFunc func(context.Context, error) (Classification, error)

// Classify invokes the adapted function.
func (function ClassifyFunc) Classify(ctx context.Context, err error) (Classification, error) {
	return function(ctx, err)
}

// Config contains all dependencies and bounds for a Policy. MaxAttempts is
// mandatory so no policy can retry forever.
type Config struct {
	Backoff        Backoff
	MaxAttempts    uint
	MaxElapsed     time.Duration
	AttemptTimeout time.Duration
	MinDelay       time.Duration
	MaxDelay       time.Duration
	MaxSleep       time.Duration
	Clock          Clock
	Sleeper        Sleeper
	Random         Random
	Classifier     Classifier
	Observer       Observer
	HistoryLimit   uint
}

// Policy is an immutable, explicitly bounded retry policy.
type Policy struct {
	config Config
}

// NewPolicy validates and copies config.
func NewPolicy(config Config) (*Policy, error) {
	switch {
	case nilLike(config.Backoff):
		return nil, fmt.Errorf("%w: backoff is required", ErrInvalidPolicy)
	case config.MaxAttempts == 0:
		return nil, fmt.Errorf("%w: max attempts must be positive", ErrInvalidPolicy)
	case nilLike(config.Clock):
		return nil, fmt.Errorf("%w: clock is required", ErrInvalidPolicy)
	case nilLike(config.Sleeper):
		return nil, fmt.Errorf("%w: sleeper is required", ErrInvalidPolicy)
	case nilLike(config.Classifier):
		return nil, fmt.Errorf("%w: classifier is required", ErrInvalidPolicy)
	case config.MaxElapsed < 0 || config.AttemptTimeout < 0 || config.MinDelay < 0 || config.MaxDelay < 0 || config.MaxSleep < 0:
		return nil, fmt.Errorf("%w: durations cannot be negative", ErrInvalidPolicy)
	case config.MaxDelay > 0 && config.MinDelay > config.MaxDelay:
		return nil, fmt.Errorf("%w: minimum delay exceeds maximum delay", ErrInvalidPolicy)
	case config.HistoryLimit > MaxHistoryEntries:
		return nil, fmt.Errorf("%w: history limit exceeds %d", ErrInvalidPolicy, MaxHistoryEntries)
	default:
		if nilLike(config.Random) {
			config.Random = nil
		}
		if nilLike(config.Observer) {
			config.Observer = nil
		}
		return &Policy{config: config}, nil
	}
}

func nilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	kind := reflected.Kind()
	if kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface ||
		kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice {
		return reflected.IsNil()
	}
	return false
}

func (policy *Policy) delay(attempt uint, previous time.Duration) time.Duration {
	delay := nonNegative(policy.config.Backoff.Delay(attempt, previous, policy.config.Random))
	return policy.boundDelay(delay)
}

func (policy *Policy) boundDelay(delay time.Duration) time.Duration {
	delay = nonNegative(delay)
	if delay < policy.config.MinDelay {
		delay = policy.config.MinDelay
	}
	if policy.config.MaxDelay > 0 && delay > policy.config.MaxDelay {
		delay = policy.config.MaxDelay
	}
	return delay
}

func (policy *Policy) attemptContext(parent context.Context, start time.Time) (context.Context, context.CancelFunc, BudgetKind) {
	timeout := policy.config.AttemptTimeout
	kind := BudgetAttempt
	if policy.config.MaxElapsed > 0 {
		remaining := policy.config.MaxElapsed - elapsed(policy, start)
		if timeout == 0 || remaining <= timeout {
			timeout = remaining
			kind = BudgetElapsed
		}
	}
	if timeout == 0 {
		return parent, func() {}, kind
	}
	if clock, ok := policy.config.Clock.(TimeoutClock); ok {
		ctx, cancel := clock.WithTimeout(parent, timeout)
		return ctx, cancel, kind
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, kind
}
