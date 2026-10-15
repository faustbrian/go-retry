package retryhttp_test

import (
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	retryhttp "github.com/faustbrian/go-retry/v2/adapters/http"
)

func TestParseRetryAfterWithOptionsPreservesFiniteDeltaAcceptance(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	options := retryhttp.RetryAfterOptions{MaxBytes: 1 << 20, MaxDeltaSeconds: 2147483647}
	for _, test := range []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"", 0, false}, {"  ", 0, false}, {"0", 0, true}, {"1", time.Second, true},
		{" 5 ", 5 * time.Second, true}, {"60", time.Minute, true}, {"61", 61 * time.Second, true},
		{"2147483647", 2147483647 * time.Second, true}, {"2147483648", 0, false},
		{"9223372037", 0, false}, {strings.Repeat("9", 100), 0, false},
		{"-1", 0, false}, {"+1", 0, false}, {"1.5", 0, false}, {"1, 2", 0, false},
		{strings.Repeat(" ", 128) + "5", 5 * time.Second, true},
		{strings.Repeat("0", 128) + "5", 5 * time.Second, true},
	} {
		got, ok := retryhttp.ParseRetryAfterWithOptions(test.value, now, options)
		if got != test.want || ok != test.ok {
			t.Errorf("parse %q = (%v,%t), want (%v,%t)", test.value, got, ok, test.want, test.ok)
		}
	}
}

func TestParseRetryAfterWithOptionsRetainsDateAndLocationSemantics(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	// A delta-seconds ceiling must not become a ceiling on valid HTTP dates.
	options := retryhttp.RetryAfterOptions{MaxDeltaSeconds: 1}
	for _, delta := range []time.Duration{-time.Second, 0, time.Second, 59 * time.Second, time.Minute, 61 * time.Second} {
		value := now.Add(delta).Format(http.TimeFormat)
		for _, clock := range []time.Time{now, now.In(time.FixedZone("offset", 3600))} {
			got, ok := retryhttp.ParseRetryAfterWithOptions(value, clock, options)
			if !ok || got != max(delta, 0) {
				t.Errorf("date %q = (%v,%t), want %v", value, got, ok, max(delta, 0))
			}
		}
	}
	for _, value := range []string{"Sunday, 19-Jul-26 12:00:05 GMT", "Sun Jul 19 12:00:05 2026"} {
		got, ok := retryhttp.ParseRetryAfterWithOptions(value, now, options)
		if !ok || got != 5*time.Second {
			t.Errorf("obsolete date %q = (%v,%t)", value, got, ok)
		}
	}
}

func TestParseRetryAfterWithOptionsKeepsRawBoundsAndDefaultContract(t *testing.T) {
	now := time.Unix(0, 0)
	for _, size := range []int{127, 128, 129, 1 << 20} {
		value := strings.Repeat("0", size-1) + "5"
		got, ok := retryhttp.ParseRetryAfterWithOptions(value, now, retryhttp.RetryAfterOptions{MaxBytes: size, MaxDeltaSeconds: 2147483647})
		if !ok || got != 5*time.Second {
			t.Errorf("inclusive raw bound %d = (%v,%t)", size, got, ok)
		}
		if _, ok := retryhttp.ParseRetryAfterWithOptions(value+" ", now, retryhttp.RetryAfterOptions{MaxBytes: size}); ok {
			t.Errorf("raw bound %d applied after trimming", size)
		}
	}
	for _, maximum := range []int{-1, (1 << 20) + 1} {
		if _, ok := retryhttp.ParseRetryAfterWithOptions("5", now, retryhttp.RetryAfterOptions{MaxBytes: maximum}); ok {
			t.Errorf("invalid maximum %d accepted", maximum)
		}
	}
	for _, value := range []string{"0", "120", strings.Repeat("9", 100), strings.Repeat("0", 128) + "5"} {
		got, ok := retryhttp.ParseRetryAfterWithOptions(value, now, retryhttp.RetryAfterOptions{})
		original, originalOK := retryhttp.ParseRetryAfter(value, now)
		if got != original || ok != originalOK {
			t.Errorf("zero options %q = (%v,%t), original (%v,%t)", value, got, ok, original, originalOK)
		}
	}
	long := strings.Repeat("0", 128) + "5"
	if _, ok := retryhttp.ParseRetryAfter(long, now); ok {
		t.Fatal("default 128-byte parsing boundary widened")
	}
	if _, err := retryhttp.NewError(429, http.Header{"Retry-After": {long}}, nil); !errors.Is(err, retryhttp.ErrInvalidResponse) {
		t.Fatalf("retained metadata bound changed: %v", err)
	}
	if got, ok := retryhttp.ParseRetryAfterWithOptions("18446744073709551615", now, retryhttp.RetryAfterOptions{MaxDeltaSeconds: math.MaxUint64}); !ok || got != time.Duration(math.MaxInt64) {
		t.Fatalf("large allowed delta = (%v,%t)", got, ok)
	}
	if _, ok := retryhttp.ParseRetryAfterWithOptions("18446744073709551616", now, retryhttp.RetryAfterOptions{MaxDeltaSeconds: math.MaxUint64}); ok {
		t.Fatal("delta larger than configured ceiling accepted")
	}
}
