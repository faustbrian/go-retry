// Package retryotel adapts bounded retry observations to caller-owned
// OpenTelemetry metrics without recording operation errors or values.
// Its import path is github.com/faustbrian/go-retry/v3/adapters/otel. New borrows
// a provider and creates synchronous instruments; nil/zero observers are no-ops.
// Metrics count notifications, not a complete dispatch audit. The caller owns
// low-cardinality labels, reader/exporter lifetime and shutdown. No global
// provider, asynchronous instrument callback or exporter is created.
package retryotel
