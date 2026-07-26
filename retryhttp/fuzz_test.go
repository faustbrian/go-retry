package retryhttp_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/retry/retryhttp"
)

func FuzzParseRetryAfterNeverReturnsNegativeDelay(fuzz *testing.F) {
	fuzz.Add("120", int64(0))
	fuzz.Add("Sun, 19 Jul 2026 12:02:00 GMT", time.Now().UnixNano())
	fuzz.Add("invalid", int64(1<<63-1))
	fuzz.Fuzz(func(t *testing.T, value string, nowNanoseconds int64) {
		delay, ok := retryhttp.ParseRetryAfter(value, time.Unix(0, nowNanoseconds))
		if ok && delay < 0 {
			t.Fatalf("negative Retry-After delay %s", delay)
		}
	})
}
