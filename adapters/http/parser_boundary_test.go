package retryhttp_test

import (
	"strings"
	"testing"
	"time"

	retryhttp "github.com/faustbrian/go-retry/v3/adapters/http"
)

func TestRetryAfterBoundsRawInputBeforeParsing(t *testing.T) {
	for _, prefix := range []string{" ", "0"} {
		accepted := strings.Repeat(prefix, retryhttp.MaxRetryAfterBytes-1) + "1"
		if d, ok := retryhttp.ParseRetryAfter(accepted, time.Time{}); !ok || d != time.Second {
			t.Fatal("exact raw-byte boundary rejected")
		}
		rejected := strings.Repeat(prefix, retryhttp.MaxRetryAfterBytes) + "1"
		if d, ok := retryhttp.ParseRetryAfter(rejected, time.Time{}); ok || d != 0 {
			t.Fatal("over-limit raw input accepted")
		}
	}
}
