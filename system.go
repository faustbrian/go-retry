package retry

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// SystemClock uses the process monotonic wall clock. It contains no global
// mutable state.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// WithTimeout derives a standard context timeout.
func (SystemClock) WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// SystemSleeper waits with a context-owned timer and always stops the timer.
type SystemSleeper struct{}

// Sleep waits for delay or context cancellation.
func (SystemSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// SeededRandom is a deterministic, concurrency-safe PCG random source.
type SeededRandom struct {
	mu     sync.Mutex
	random *rand.Rand
}

// NewRandom constructs a deterministic random source from explicit seeds.
func NewRandom(seed1, seed2 uint64) *SeededRandom {
	// #nosec G404 -- deterministic jitter is not a cryptographic source.
	return &SeededRandom{random: rand.New(rand.NewPCG(seed1, seed2))}
}

// Int64n returns a uniform value in [0, upper).
func (random *SeededRandom) Int64n(upper int64) int64 {
	if upper <= 0 {
		return 0
	}
	random.mu.Lock()
	defer random.mu.Unlock()
	return random.random.Int64N(upper)
}

var _ TimeoutClock = SystemClock{}
var _ Sleeper = SystemSleeper{}
var _ Random = (*SeededRandom)(nil)
