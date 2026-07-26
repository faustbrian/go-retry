# retry

`retry` is a dependency-light foundation for bounded retry execution and
backoff. Every policy requires a finite attempt limit, an error classifier,
timing dependencies, a backoff strategy, and an operation. The package never
assumes the operation is idempotent or safe to repeat.

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

value, result, err := retry.Do(ctx, policy, func(ctx context.Context) (string, error) {
    value, err := readOnce(ctx)
    if isTransient(err) {
        return "", retry.Retryable(err)
    }
    return value, retry.Permanent(err)
})
```

The caller must decide whether `readOnce` is safe to repeat. Marking an error
retryable classifies a failure; it does not make a side effect idempotent.

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

## Boundaries

This module owns no circuit state, rate limits, queues, schedules,
idempotency keys, global policy, global random source, metrics registry, or
background worker. Operation panics propagate and are never retried.

## License

MIT. See [LICENSE](LICENSE).
