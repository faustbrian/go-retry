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
