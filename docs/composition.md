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
