package retryhttp_test

import (
	"net/http"
	"testing"
	"time"

	retryhttp "github.com/faustbrian/go-retry/v3/adapters/http"
)

func TestRetryAfterDateAndLocationSemantics(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	for _, delta := range []time.Duration{-time.Second, 0, time.Second, 59 * time.Second, time.Minute, 61 * time.Second} {
		value := now.Add(delta).Format(http.TimeFormat)
		for _, clock := range []time.Time{now, now.In(time.FixedZone("offset", 3600))} {
			got, ok := retryhttp.ParseRetryAfter(value, clock)
			if !ok || got != max(delta, 0) {
				t.Errorf("date %q = (%v,%t), want %v", value, got, ok, max(delta, 0))
			}
		}
	}
	for _, value := range []string{"Sunday, 19-Jul-26 12:00:05 GMT", "Sun Jul 19 12:00:05 2026"} {
		got, ok := retryhttp.ParseRetryAfter(value, now)
		if !ok || got != 5*time.Second {
			t.Errorf("obsolete date %q = (%v,%t)", value, got, ok)
		}
	}
}
