# Hardening Goal: Retry Policies

## Objective

Prove delay algorithms, budgets, cancellation, classification, randomness,
observation, and adapters safe under hostile configuration and failure storms.

## Required Audits

- Exhaust zero, negative, minimum, maximum, overflowing, and contradictory
  attempt/duration/delay settings.
- Verify exact deterministic vectors for every non-random strategy.
- Statistically test jitter bounds and bias using injected deterministic
  sources.
- Exercise cancellation before, during, and after attempts/sleeps and deadline
  races.
- Verify classifiers, observers, panics, wrapped errors, Retry-After dates and
  seconds, clock skew, and malformed headers.
- Prove no hidden retry of permanent or non-idempotent work.
- Race shared immutable policies; detect timer and goroutine leaks.
- Fuzz configuration and HTTP adapters; mutation-test bounds, attempt counts,
  error classification, and cancellation.

## Release Blockers

- Infinite retry, ignored cancellation, overflowed delay, biased/out-of-range
  jitter, incorrect attempt count, swallowed cause, timer leak, race, or hidden
  repeat-safety assumption.

## Completion Criteria

- Vector, statistical, cancellation, adapter, fuzz, race, mutation, leak, and
  benchmark suites pass with meaningful 100% coverage.

