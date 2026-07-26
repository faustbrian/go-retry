package retry_test

import (
	"testing"
	"time"

	retry "github.com/faustbrian/golib/pkg/retry"
)

func TestJitterMeansRemainUnbiasedWithDeterministicSource(t *testing.T) {
	t.Parallel()

	const samples = 100_000
	tests := []struct {
		name     string
		backoff  retry.Backoff
		previous time.Duration
		wantMean float64
	}{
		{"full", retry.FullJitter(retry.Constant(1000)), 0, 500},
		{"equal", retry.EqualJitter(retry.Constant(1000)), 0, 750},
		{"exponential", retry.ExponentialJitter(1000, 2, 0.25), 0, 1000},
		{"decorrelated", retry.DecorrelatedJitter(1000), 1000, 2000},
	}
	seeds := [][2]uint64{{1, 10}, {2, 11}, {3, 12}, {4, 13}}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			random := retry.NewRandom(seeds[index][0], seeds[index][1])
			total := int64(0)
			for range samples {
				total += int64(test.backoff.Delay(1, test.previous, random))
			}
			mean := float64(total) / samples
			if difference := mean - test.wantMean; difference < -5 || difference > 5 {
				t.Fatalf("mean %.2f outside deterministic tolerance around %.2f", mean, test.wantMean)
			}
		})
	}
}
