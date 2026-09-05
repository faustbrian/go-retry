# Composition

The ecosystem does not freeze one resilience policy order in Phase 3. A stack
may place retry inside a circuit breaker so each attempt is visible to circuit
state and outside a rate limiter so each attempt consumes a permit, but other
systems require the reverse. Record and test the selected ownership order. The
supported stack order is a Phase 4 composition decision.

```text
caller deadline
  -> retry policy
       -> rate-limit permit per attempt
            -> circuit-breaker admission per attempt
                 -> operation
```

`retry` does not import or configure `rate-limit`,
`circuit-breaker`, queue schedulers, or idempotency storage. Avoid nested
automatic retries in HTTP clients, database drivers, or SDKs unless their
combined attempt and elapsed bounds are calculated explicitly.

When retry and hedge are nested, both must enable their resilience-budget mode
and receive the same scoped context. The outer executor owns the current
physical attempt; the inner executor reuses it, while every retry or hedge asks
the shared scope for a new additional-work permit. This changes the upper bound
from an accidental `(retries + 1) * (hedges + 1)` to the original attempt plus
the configured shared additional-work allowance.

## Collaborator ownership

`NewPolicyStrict` copies the `Config` value and its scalar bounds. Mutating the
original config after construction does not reconfigure the policy. The
configured `Backoff`, `Clock`, `Sleeper`, `Random`, `Classifier`, and `Observer`
values remain caller-owned references borrowed for the policy's lifetime. A
policy may invoke them concurrently when callers use that policy concurrently,
so their implementations and any captured mutable state must support that use
or be synchronized by the caller. Do not mutate or release their backing state
until the last policy execution has returned. The retry package never closes
or shuts down these collaborators.

All collaborator calls are synchronous: blocking blocks the current execution,
separate policy executions may invoke the same collaborator concurrently, and
the package holds no lock across a call, so re-entry is allowed. Callback
arguments and results are borrowed only for the call. A known operation error
is the explicit exception: its identity remains machine-reachable through a
terminal safe-cause carrier and, on the documented released rows, through
bounded `Result.History`.
`Clock.Now`, `Backoff.Delay`, and `Random.Int64n` have no cancellation input;
`TimeoutClock.WithTimeout`, `Sleeper.Sleep`, and `Classifier.Classify` receive
the documented execution context. Panics propagate except that `DoStrict`
sanitizes classifier panics and both `Do` and `DoStrict` isolate observer
panics. A retryable error's `DelayHint.RetryDelay` is called at most once and
has no cancellation input. Every admitted work permit is completed exactly
once: after the dispatched operation, or before return when cancellation, an
elapsed-budget exit, or a panic prevents dispatch. Completion errors are
ignored, and completion panics propagate.

The integration adapters follow the same rule for retained references:

- `retryadapter` and `adapters/http` retain caller predicates for the classifier's
  lifetime. The caller owns captured state and its synchronization and cleanup.
- `adapters/slog` retains the supplied `*slog.Logger`. The caller owns its handler
  and must keep the logging pipeline usable while the observer is in use.
- `adapters/otel` creates and retains instruments from the supplied meter
  provider. The caller owns the provider and must delay provider shutdown until
  every observer call has completed.

`adapters/http.New` defensively copies `RetryStatuses`, and
`adapters/http.NewError` copies the bounded `Retry-After` string instead of
retaining the header map. Its optional transient callback is retained for the
classifier lifetime and receives the exact borrowed error synchronously, at
most once, only after successor-error recognition misses. The classifier
retains neither that error nor the callback result and holds no lock across the
call, so re-entry is allowed, blocking blocks the caller, separate calls may
invoke it concurrently, and panics propagate. The callback has no context
parameter, so caller cancellation cannot preempt an invocation already in
progress.

Operation functions passed to `DoStrict` are borrowed only for that call and
invoked once per dispatched attempt with the attempt context. The package
starts no background work and does not invoke them after return.
