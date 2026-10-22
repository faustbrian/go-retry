// Package retryslog adapts bounded retry observations to caller-owned slog
// logging without recording operation errors or values.
// Its import path is github.com/faustbrian/go-retry/v3/adapters/slog. New borrows
// a caller logger; nil/zero observers are no-ops. Direct calls are synchronous
// with Background and require handlers safe for overlapping/reentrant use.
// The caller owns handler lifetime and redacted, low-cardinality policy labels.
package retryslog
