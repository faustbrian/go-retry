# Composition

Retry should normally sit inside a circuit breaker so each attempt is visible
to circuit state, and outside a rate limiter when every attempt must consume a
permit. Different systems may need the reverse; write the ownership order down
and test it.

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

`NewPolicy` copies the `Config` value and its scalar bounds. Mutating the
original config after construction does not reconfigure the policy. The
configured `Backoff`, `Clock`, `Sleeper`, `Random`, `Classifier`, and `Observer`
values remain caller-owned references borrowed for the policy's lifetime. A
policy may invoke them concurrently when callers use that policy concurrently,
so their implementations and any captured mutable state must support that use
or be synchronized by the caller. Do not mutate or release their backing state
until the last policy execution has returned. The retry package never closes
or shuts down these collaborators.

The integration adapters follow the same rule for retained references:

- `retryadapter` and `retryhttp` retain caller predicates for the classifier's
  lifetime. The caller owns captured state and its synchronization and cleanup.
- `retrylog` retains the supplied `*slog.Logger`. The caller owns its handler
  and must keep the logging pipeline usable while the observer is in use.
- `retrytelemetry` creates and retains instruments from the supplied meter
  provider. The caller owns the provider and must delay provider shutdown until
  every observer call has completed.

`retryhttp.NewClassifier` defensively copies `RetryStatuses`, and
`retryhttp.StatusError` copies the `Retry-After` string instead of retaining the
header map. Operation functions passed to `Do` are borrowed only for that call;
the package starts no background work and does not invoke them after `Do`
returns.
