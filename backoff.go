// Package retry executes explicitly classified operations under bounded retry
// policies. Start with a constructed Policy and DoStrict when dispatch outcome
// matters, or Do for its compatibility cancellation rules. It never decides
// idempotency or undoes side effects. Attempt limits are mandatory; duration
// budgets and shared resilience admission are explicit opt-ins.
//
// Policies borrow collaborators and captured state. Calls are synchronous, may
// overlap or reenter and are not forcibly preempted. Built-in strategies have
// immutable state; consumers synchronize custom collaborators and RNGs. There
// are no package workers or shutdown lifecycle. HTTP, PostgreSQL, logging and
// telemetry adapters have distinct canonical and compatibility contracts; the
// module includes their external dependencies, while the core imports resilience.
// No retry-owned transport, database pool or telemetry exporter is created.
package retry

import (
	"math"
	"math/bits"
	"time"
)

// Random is an injected, concurrency-safe source of uniform integers in
// [0, upper). Implementations must return zero when upper is non-positive.
type Random interface {
	// Int64n must return a uniform sample for positive upper. It may be called
	// concurrently by a shared policy; it is not a cryptographic interface.
	Int64n(upper int64) int64
}

// Backoff computes the delay before retry attempt. Attempt starts at one for
// the first retry. Previous is the previously selected delay and is used only
// by state-dependent strategies such as decorrelated jitter.
type Backoff interface {
	// Delay receives the failed operation ordinal (1 before the first retry)
	// and previous fully selected delay. It must bound its synchronous work;
	// returned negative durations are clamped by Policy. Calls may overlap.
	Delay(attempt uint, previous time.Duration, random Random) time.Duration
}

type backoffFunc func(uint, time.Duration, Random) time.Duration

func (function backoffFunc) Delay(attempt uint, previous time.Duration, random Random) time.Duration {
	return function(attempt, previous, random)
}

// Constant returns the same non-negative delay for every attempt.
func Constant(delay time.Duration) Backoff {
	delay = nonNegative(delay)
	return backoffFunc(func(uint, time.Duration, Random) time.Duration { return delay })
}

// Linear returns initial + (attempt-1)*increment, using saturating arithmetic.
func Linear(initial, increment time.Duration) Backoff {
	initial, increment = nonNegative(initial), nonNegative(increment)
	return backoffFunc(func(attempt uint, _ time.Duration, _ Random) time.Duration {
		if attempt == 0 {
			return initial
		}
		return saturatingAdd(initial, saturatingMultiply(increment, uint64(attempt-1)))
	})
}

// Polynomial returns base + coefficient*attempt^power with saturation.
func Polynomial(base, coefficient time.Duration, power uint) Backoff {
	base, coefficient = nonNegative(base), nonNegative(coefficient)
	return backoffFunc(func(attempt uint, _ time.Duration, _ Random) time.Duration {
		return saturatingAdd(base, saturatingMultiply(coefficient, saturatingPower(uint64(attempt), power)))
	})
}

// Fibonacci returns unit multiplied by the attempt-th Fibonacci number, where
// the first two retry delays both equal unit.
func Fibonacci(unit time.Duration) Backoff {
	unit = nonNegative(unit)
	return backoffFunc(func(attempt uint, _ time.Duration, _ Random) time.Duration {
		attempt = min(attempt, maxFibonacciAttempt)
		previous, current := uint64(0), uint64(1)
		for index := uint(1); index < attempt; index++ {
			previous, current = current, previous+current
			if saturatingMultiply(unit, current) == maxDuration {
				return maxDuration
			}
		}
		return saturatingMultiply(unit, current)
	})
}

// Exponential returns initial*multiplier^(attempt-1) with saturation.
func Exponential(initial time.Duration, multiplier uint64) Backoff {
	initial = nonNegative(initial)
	return backoffFunc(func(attempt uint, _ time.Duration, _ Random) time.Duration {
		if attempt == 0 {
			return initial
		}
		return saturatingMultiply(initial, saturatingPower(multiplier, attempt-1))
	})
}

// FullJitter borrows backoff and chooses between zero and its nonnegative delay.
// Nil backoff yields zero; nil randomness selects zero after the wrapped delay
// is computed. A typed-nil implementation can still panic. Randomness must meet
// Random's uniformity contract.
func FullJitter(backoff Backoff) Backoff {
	return backoffFunc(func(attempt uint, previous time.Duration, random Random) time.Duration {
		if backoff == nil {
			return 0
		}
		return randomDuration(0, nonNegative(backoff.Delay(attempt, previous, random)), random)
	})
}

// EqualJitter retains half of the wrapped delay and uniformly jitters the
// remainder. Nil backoff yields zero; nil randomness selects the lower bound.
// The wrapped implementation and random source are borrowed.
func EqualJitter(backoff Backoff) Backoff {
	return backoffFunc(func(attempt uint, previous time.Duration, random Random) time.Duration {
		if backoff == nil {
			return 0
		}
		delay := nonNegative(backoff.Delay(attempt, previous, random))
		return randomDuration(delay/2, delay, random)
	})
}

// ExponentialJitter applies centered proportional jitter to exponential
// backoff. Finite Factor is clamped to [0, 1]; NaN is not validated and
// has no promised jitter range.
func ExponentialJitter(initial time.Duration, multiplier uint64, factor float64) Backoff {
	factor = min(max(factor, 0), 1)
	exponential := Exponential(initial, multiplier)
	return backoffFunc(func(attempt uint, previous time.Duration, random Random) time.Duration {
		delay := exponential.Delay(attempt, previous, random)
		variation := time.Duration(float64(delay) * factor)
		return randomDuration(delay-variation, saturatingAdd(delay, variation), random)
	})
}

// DecorrelatedJitter chooses uniformly between base and three times the
// previous delay. The first retry uses base as its previous delay.
func DecorrelatedJitter(base time.Duration) Backoff {
	base = nonNegative(base)
	return backoffFunc(func(_ uint, previous time.Duration, random Random) time.Duration {
		previous = max(nonNegative(previous), base)
		return randomDuration(base, saturatingMultiply(previous, 3), random)
	})
}

const (
	maxDuration         = time.Duration(math.MaxInt64)
	maxFibonacciAttempt = uint(93) // F(93) exceeds the maximum duration.
)

func nonNegative(value time.Duration) time.Duration {
	return max(value, 0)
}

func saturatingAdd(left, right time.Duration) time.Duration {
	// #nosec G115 -- callers normalize retry durations before saturation.
	sum, carry := bits.Add64(uint64(left), uint64(right), 0)
	if carry != 0 || sum>>63 != 0 {
		return maxDuration
	}
	return time.Duration(sum)
}

func saturatingMultiply(value time.Duration, multiplier uint64) time.Duration {
	// #nosec G115 -- callers normalize retry durations before saturation.
	high, low := bits.Mul64(uint64(value), multiplier)
	if high != 0 || low>>63 != 0 {
		return maxDuration
	}
	return time.Duration(low)
}

func saturatingPower(base uint64, exponent uint) uint64 {
	result := uint64(1)
	for range bits.Len(exponent) {
		if exponent&1 != 0 {
			result = saturatingUintMultiply(result, base)
		}
		exponent >>= 1
		if exponent != 0 {
			base = saturatingUintMultiply(base, base)
		}
	}
	return result
}

func saturatingUintMultiply(left, right uint64) uint64 {
	high, low := bits.Mul64(left, right)
	if high != 0 {
		return math.MaxUint64
	}
	return low
}

// randomDuration normally samples inclusive integer-nanosecond endpoints. A
// full MaxInt64 span excludes the upper endpoint to keep Int64n upper positive.
// Normalize faulty RNG offsets defensively; this does not restore uniformity.
func randomDuration(minimum, maximum time.Duration, random Random) time.Duration {
	if random == nil {
		return minimum
	}
	span := int64(maximum - minimum)
	if span == 0 || span>>63 != 0 {
		return minimum
	}
	upper := span
	if span != math.MaxInt64 {
		upper++
	}
	offset := random.Int64n(upper)
	offset = offset % upper
	sign := offset >> 63
	offset = (offset ^ sign) - sign
	return minimum + time.Duration(offset)
}
