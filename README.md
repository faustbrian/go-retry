# retry

[![CI](https://github.com/faustbrian/go-retry/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/faustbrian/go-retry/actions/workflows/ci.yml)
[![Documentation](https://img.shields.io/badge/docs-adoption_and_API-blue)](docs/index.md)
[![Go Reference](https://pkg.go.dev/badge/github.com/faustbrian/go-retry/v3.svg)](https://pkg.go.dev/github.com/faustbrian/go-retry/v3)
[![Release](https://img.shields.io/github/v/release/faustbrian/go-retry?sort=semver)](https://github.com/faustbrian/go-retry/releases)
[![Go](https://img.shields.io/badge/go-1.27.0-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

`retry` is a dependency-light foundation for bounded retry execution and
backoff. Every policy requires a finite attempt limit, an error classifier,
timing dependencies, a backoff strategy, and an operation. The package never
assumes the operation is idempotent or safe to repeat.

The v3 source uses the `/v3` module path and requires Go 1.27.0. It has one
explicit-outcome executor and canonical adapters, without compatibility paths.
See the [major-version migration](docs/migration.md) before adoption. Public
tags and releases establish availability; source on main is not publication.

```go
policy, err := retry.NewPolicy(retry.Config{
    Backoff: retry.FullJitter(retry.Exponential(100*time.Millisecond, 2)),
    MaxAttempts: 4,
    MaxElapsed: 3*time.Second,
    MaxDelay: time.Second,
    Clock: retry.SystemClock{},
    Sleeper: retry.SystemSleeper{},
    Random: retry.NewRandom(1, 2),
    Classifier: retry.RetryableClassifier(),
})
if err != nil {
    return err
}

result, err := retry.Do(ctx, policy, func(ctx context.Context) (retry.AttemptResult[string], error) {
	value, err := readOnce(ctx)
	if isTransient(err) {
		return retry.AttemptResult[string]{Outcome: retry.OutcomeKnown}, retry.Retryable(err)
	}
	return retry.AttemptResult[string]{Value: value, Outcome: retry.OutcomeKnown}, retry.Permanent(err)
})
```

Install the root module with `go get github.com/faustbrian/go-retry/v3@latest`.
The caller must decide whether `readOnce` is safe to repeat. If dispatch occurs
but the result cannot be proved, return `OutcomeUnknown`; `Do` stops
without retrying and callers must reconcile the side effect. Marking an error
retryable classifies a known failure; it does not make a side effect idempotent.

## Adoption entry points

The root module is `github.com/faustbrian/go-retry/v3` (stable).
It requires Go 1.27.0. Select a published v3 release using the native Go module
resolver; do not use a sibling replacement or unpublished revision.
Root subpackages share this module; they need no separate installation.

Start with the [documentation map](docs/index.md) for package selection,
examples, operational constraints, and contributor references.

## Features

- Constant, linear, polynomial, Fibonacci, exponential, full-jitter,
  equal-jitter, exponential-jitter, and decorrelated-jitter backoff.
- Maximum attempts plus elapsed, attempt, delay, and total-sleep budgets.
- Injected clock, sleeper, random source, classifier, and observer.
- Typed permanent, retryable, exhausted, canceled, and budget errors.
- Generic value-returning execution with bounded result history.
- HTTP `Retry-After`, pgx SQLSTATE, domain-predicate, slog, and OpenTelemetry
  adapters.
- Deterministic vectors, statistical tests, fuzzing, race/leak checks,
  mutation checks, and comparative allocation benchmarks.

## Development

Start with `go test ./...` or a focused package/test selector. `make check` and
`make ci` use bounded checks with the public verifier declared in `.golib.yaml`,
including the named concurrency/lifecycle race assertions. See
[contributor verification](CONTRIBUTING.md#verification) for focused fuzzing,
optional local mutation and explicit full/release qualification.

## Documentation

- [Strategy selection](docs/strategies.md)
- [Idempotency and ownership](docs/idempotency.md)
- [Budgets and cancellation](docs/budgets.md)
- [HTTP and PostgreSQL adapters](docs/adapters.md)
- [Rate-limit and circuit-breaker composition](docs/composition.md)
- [Observability](docs/observability.md)
- [Migration](docs/migration.md)
- [Operations](docs/operations.md)
- [Compatibility](docs/compatibility.md)
- [FAQ](docs/faq.md)
- [Verification](docs/verification.md)

## Package map

- `retry` owns strict bounded execution, outcomes, policies, and backoff.
- `adapters/http` validates HTTP classification and bounded response metadata.
- `adapters/postgres` classifies PostgreSQL failures with caller-context
  precedence.
- `adapters/slog` and `adapters/otel` export bounded observations.
- `retryadapter` remains the supported generic predicate adapter for queue,
  webhook, filesystem, and object-storage domains.
The former `retryhttp`, `retrypgx`, `retrylog` and `retrytelemetry` packages
are removed. `retryadapter` is an active explicit domain-predicate boundary,
not a legacy alias.

Use this module when one caller explicitly owns a finite retry policy and the
replay decision. Do not use it to infer idempotency, hide driver retries, or
apply one implicit resilience stack across unrelated operations.

For ecosystem-wide selection and ownership guidance, see the versioned
[Golib ecosystem index](https://github.com/faustbrian/go-library-tools/blob/v1.8.4/docs/ecosystem/README.md)
and its [Resilience family](https://github.com/faustbrian/go-library-tools/blob/v1.8.4/docs/ecosystem/design-language.md#package-families-and-selection).

## Boundaries

This module owns no circuit state, rate limits, queues, schedules,
idempotency keys, global policy, global random source, metrics registry, or
background worker. Operation panics propagate and are never retried.

## License

MIT. See [LICENSE](LICENSE).
