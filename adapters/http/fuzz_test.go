package retryhttp_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	retryhttp "github.com/faustbrian/go-retry/adapters/http"
	//lint:ignore SA1019 Legacy parity is the compatibility contract under test.
	legacy "github.com/faustbrian/go-retry/retryhttp" //nolint:staticcheck // Legacy parity is the compatibility contract under test.
)

func FuzzParseRetryAfterNeverReturnsNegativeDelay(f *testing.F) {
	f.Add("120", int64(0))
	f.Add("Sun, 19 Jul 2026 12:02:00 GMT", int64(1_000_000_000))
	f.Add("999999999999999999999999999999", int64(-1))
	f.Add("-1", int64(1))
	f.Fuzz(func(t *testing.T, value string, unixNano int64) {
		now := time.Unix(0, unixNano)
		delay, ok := retryhttp.ParseRetryAfter(value, now)
		legacyDelay, legacyOK := legacy.ParseRetryAfter(value, now)
		if delay != legacyDelay || ok != legacyOK {
			t.Fatalf("successor=(%s,%v) legacy=(%s,%v)", delay, ok, legacyDelay, legacyOK)
		}
		if ok && delay < 0 {
			t.Fatalf("negative accepted delay %s", delay)
		}
	})
}

func FuzzNewErrorBoundsResponseMetadata(f *testing.F) {
	f.Add(503, "120")
	f.Add(99, "")
	f.Add(503, string(make([]byte, retryhttp.MaxRetryAfterBytes+1)))
	f.Fuzz(func(t *testing.T, status int, retryAfter string) {
		cause := errors.New("borrowed cause")
		value, err := retryhttp.NewError(status, http.Header{"Retry-After": {retryAfter}}, cause)
		switch {
		case status < 100 || status > 999:
			if value != nil || !errors.Is(err, retryhttp.ErrInvalidResponse) || err.Error() != "invalid HTTP retry response: status must be between 100 and 999" {
				t.Fatalf("invalid status = (%v,%v)", value, err)
			}
		case len(retryAfter) > retryhttp.MaxRetryAfterBytes:
			if value != nil || !errors.Is(err, retryhttp.ErrInvalidResponse) || err.Error() != "invalid HTTP retry response: Retry-After exceeds 128 bytes" {
				t.Fatalf("oversized header = (%v,%v)", value, err)
			}
		default:
			//nolint:errorlint // Accepted responses must retain the exact supplied cause.
			if err != nil || value == nil || value.StatusCode() != status || value.Unwrap() != cause || !errors.Is(value, cause) {
				t.Fatalf("accepted response = (%v,%v)", value, err)
			}
		}
	})
}
