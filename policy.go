package retry

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/faustbrian/go-resilience/v2"
)

// ErrInvalidPolicy identifies contradictory, implicit, or unbounded policies.
var ErrInvalidPolicy = errors.New("invalid retry policy")

// MaxHistoryEntries is the largest failure history retained by a policy.
const MaxHistoryEntries = 1024

// Clock supplies policy time for elapsed accounting. Implementations must be
// safe for concurrent calls when the policy is shared and should provide a
// consistent time progression. Backward elapsed differences are clamped to zero.
// Implement TimeoutClock as well to control per-attempt deadlines.
type Clock interface {
	// Now returns the time used for budgets, history and delay hints.
	Now() time.Time
}

// TimeoutClock derives cooperative deadline contexts using an injected clock.
// Calls are synchronous and may overlap; preserve caller values/cancellation.
type TimeoutClock interface {
	Clock
	// WithTimeout returns a usable non-nil child context and cancellation function.
	// The executor cancels it at the end of the attempt, including panic unwinding.
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// Sleeper waits synchronously without owning retry decisions. It must honor
// caller cancellation, bound its work and synchronize state for shared policies.
// The executor does not measure actual sleep or forcibly interrupt a call.
type Sleeper interface {
	// Sleep receives a nonnegative selected delay. Nil reports completion and
	// charges that requested duration to MaxSleep; an error stops execution.
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
// classifier itself failed and execution stops. It must be bounded and safe
// for overlapping calls when shared. Classification never establishes replay
// safety; error traversal and retained causes remain caller trust boundaries.
type Classifier interface {
	// Classify receives the caller context and exact operation error. Return
	// ClassificationPermanent or ClassificationRetryable; other results stop
	// execution as classifier failures.
	Classify(context.Context, error) (Classification, error)
}

// ClassifyFunc borrows a function as Classifier. Direct calls propagate panics;
// Do contains classifier panics with bounded terminal errors.
// A nil function is rejected as a required policy collaborator.
type ClassifyFunc func(context.Context, error) (Classification, error)

// Classify invokes the adapted function.
func (function ClassifyFunc) Classify(ctx context.Context, err error) (Classification, error) {
	return function(ctx, err)
}

// Config contains all dependencies and bounds for a Policy. MaxAttempts is
// mandatory so no policy can retry forever.
type Config struct {
	// Backoff is required; its implementation is borrowed and called synchronously.
	Backoff Backoff
	// MaxAttempts is required and includes the original call; one means no retries.
	MaxAttempts uint
	// MaxElapsed is a sampled elapsed budget; zero disables it. Attempt contexts
	// use the remaining budget, but collaborators and sleeps are not forcibly preempted.
	MaxElapsed time.Duration
	// AttemptTimeout adds a cooperative deadline per operation; zero disables it
	// unless MaxElapsed supplies a remaining deadline.
	AttemptTimeout time.Duration
	// MinDelay floors selected retry delays, including available server hints.
	MinDelay time.Duration
	// MaxDelay caps selected delays and hints; zero disables the cap.
	MaxDelay time.Duration
	// MaxSleep caps accumulated successful sleep requests; zero disables it.
	// It does not measure actual sleeper wall time.
	MaxSleep time.Duration
	// StopBeforeDeadline stops instead of sleeping when the selected delay
	// would reach the caller context deadline, measured on Clock, so callers
	// holding scarce resources get the last failure at once rather than after
	// cancellation. A custom Clock must agree with the context deadline.
	StopBeforeDeadline bool
	// Clock is required and borrowed for elapsed accounting and optional deadlines.
	Clock Clock
	// Sleeper is required and borrowed; it receives the caller context, not
	// an attempt or MaxElapsed-derived context.
	Sleeper Sleeper
	// Random is optional and borrowed; nil selects deterministic jitter lower bounds.
	Random Random
	// Classifier is required and receives the original failure and caller context,
	// not the attempt context.
	Classifier Classifier
	// Observer is optional, synchronous and borrowed. Engine calls contain panics,
	// not blocking or arbitrary side effects.
	Observer Observer
	// HistoryLimit retains the most recent recorded failures up to MaxHistoryEntries.
	// Zero disables history; error objects are borrowed and not byte-bounded.
	HistoryLimit uint
	// UseResilienceBudget consumes the shared work-amplification scope attached
	// to the execution context. Existing standalone behavior is unchanged when false.
	UseResilienceBudget bool
}

// Policy copies configuration but borrows collaborators and captured state.
// Construct it with NewPolicy; the zero Policy is unusable.
// Concurrent executions are safe only when all shared collaborators are safe;
// retry owns no worker, shutdown lifecycle, callback lock or forced preemption.
type Policy struct {
	config Config
}

func (policy *Policy) initialWorkContext(ctx context.Context) (context.Context, resilience.Attempt, resilience.Permit, error) {
	if !policy.config.UseResilienceBudget {
		return ctx, resilience.Attempt{}, nil, nil
	}
	if attempt, ok := resilience.AttemptFromContext(ctx); ok {
		return ctx, attempt, nil, nil
	}
	return resilience.AdmitAttempt(ctx, resilience.OriginOriginal, 0, policy.config.Clock.Now())
}

func (policy *Policy) retryWorkContext(ctx context.Context, parent resilience.Attempt) (context.Context, resilience.Attempt, resilience.Permit, error) {
	if !policy.config.UseResilienceBudget {
		return ctx, resilience.Attempt{}, nil, nil
	}
	return resilience.AdmitAttempt(ctx, resilience.OriginRetry, parent.Ordinal, policy.config.Clock.Now())
}

// NewPolicy validates finite attempt count, required dependencies and consistent
// nonnegative duration/history limits. Scalars are copied; dependencies are
// borrowed. Explicitly typed-nil optional Random/Observer values are rejected.
// It returns nil with an ErrInvalidPolicy-wrapping error on invalid config.
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
	case config.Random != nil && nilLike(config.Random):
		return nil, fmt.Errorf("%w: random is typed nil", ErrInvalidPolicy)
	case config.Observer != nil && nilLike(config.Observer):
		return nil, fmt.Errorf("%w: observer is typed nil", ErrInvalidPolicy)
	default:
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
	delay = max(nonNegative(delay), policy.config.MinDelay)
	if policy.config.MaxDelay != 0 {
		delay = min(delay, policy.config.MaxDelay)
	}
	return delay
}

// attemptContext chooses the tighter cooperative timeout; equal deadlines
// attribute exhaustion to MaxElapsed. Custom clocks own deadline mechanics.
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
