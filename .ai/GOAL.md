# Goal: Explicit Retry Policies And Backoff

## Objective

Build `retry` as the shared, dependency-light foundation for bounded retry
execution and backoff policies. It MUST centralize timing and classification
without hiding whether an operation is safe to repeat.

## Core Scope

- Constant, linear, polynomial, Fibonacci, exponential, full-jitter,
  equal-jitter, exponential-jitter, and decorrelated-jitter strategies.
- Maximum attempts, elapsed-time budget, per-attempt timeout, maximum delay,
  minimum delay, and total sleep budget.
- Injected clock, sleeper, random source, observer, and error classifier.
- Context cancellation and deadline precedence.
- Typed retryable, permanent, exhausted, canceled, and budget errors preserving
  causes and attempt history under bounds.
- Retry result metadata including attempts, elapsed time, final delay, and
  reason without retaining arbitrary payloads.
- Generic value-returning execution and notification hooks.
- Deterministic testing with no wall-clock sleeps.

## Adapters

- HTTP `Retry-After` and status/error classification adapter.
- PostgreSQL/pgx transient error adapter where safely classifiable.
- Queue, webhook, filesystem, and object-storage adapters owned separately from
  their business idempotency decisions.
- `log` and `telemetry` observers with bounded fields.

## Boundaries

- The package never assumes an operation is idempotent.
- It does not own circuit state (`circuit-breaker`), rate limits
  (`rate-limit`), queues, scheduling, or idempotency.
- No retry occurs unless the caller supplies a policy and execution function.
- No global random source, clock, policy, metrics, or background worker.
- Panic retry behavior is disabled by default and explicit if ever supported.

## Verification And Documentation

Require meaningful 100% production coverage, strategy vectors, statistical
jitter tests without flakiness, cancellation matrices, overflow/saturation,
classifier failures, Retry-After parsing, deterministic clocks, fuzzing, race,
mutation, and allocation benchmarks against maintained retry libraries.

Document strategy selection, idempotency requirements, budgets, HTTP/database
adapters, composition with rate limits/circuit breakers, observability,
migration, operations, FAQ, compatibility, and changelog. CI/local gates follow
ecosystem standards.

## Acceptance Criteria

- Every retry is bounded, cancelable, observable, and explicitly classified.
- Timing arithmetic cannot overflow or create negative/unbounded delays.
- Callers retain responsibility for repeat safety and side effects.
- Meaningful 100% coverage and every blocking gate pass.
