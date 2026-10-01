package retryhttp_test

import (
	"strings"
	"testing"
	"time"

	canonical "github.com/faustbrian/go-retry/v2/adapters/http"
	//lint:ignore SA1019 The retained flat parser shares the canonical input contract.
	flat "github.com/faustbrian/go-retry/v2/retryhttp" //nolint:staticcheck // The retained flat parser shares the canonical input contract.
)

func TestPublicRetryAfterParsersBoundRawInputBeforeParsing(t *testing.T) {
	for name, parse := range map[string]func(string, time.Time) (time.Duration, bool){"canonical": canonical.ParseRetryAfter, "flat": flat.ParseRetryAfter} {
		t.Run(name, func(t *testing.T) {
			for _, prefix := range []string{" ", "0"} {
				accepted := strings.Repeat(prefix, canonical.MaxRetryAfterBytes-1) + "1"
				if delay, ok := parse(accepted, time.Time{}); !ok || delay != time.Second {
					t.Fatal("exact raw-byte boundary was not accepted")
				}
				rejected := strings.Repeat(prefix, canonical.MaxRetryAfterBytes) + "1"
				if delay, ok := parse(rejected, time.Time{}); ok || delay != 0 {
					t.Fatal("over-limit raw input was parsed")
				}
			}
		})
	}
}
