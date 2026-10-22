# Major-version migration

[Documentation map](index.md) · [Repository README](../README.md)

## Version 3

Upgrade imports to `github.com/faustbrian/go-retry/v3` using a published release.
Migrate every actual application and nested consumer module, then remove old
requirements and verify the resolved graph. Historical tags remain immutable;
they are not an in-process compatibility layer.

There is one constructor, `NewPolicy`, and one executor, `Do`. The constructor
rejects explicitly typed-nil optional Random/Observer dependencies. Plain nil
means absent. The former `NewPolicyStrict` and `DoStrict` names are removed.

An operation returns `AttemptResult[T]` and an error, explicitly declaring
`OutcomeKnown` or `OutcomeUnknown`. `Do` returns `ExecutionResult[T]` and an error;
its Value is present only for known success, Retry contains metadata, and Outcome
records dispatch knowledge. Do not mechanically mark an ambiguous side effect
as known. Unknown or invalid post-dispatch outcomes stop without classification,
backoff, sleeping or replay. A known success wins a cancellation race.

Cancellation uses `ContextError` with normalized context sentinels, rather than
the removed `CanceledError`. Arbitrary cancellation and classifier-panic causes
are not disclosed or retained. Known classified causes remain machine-traversable
where documented; terminal history slices are defensively copied.

| Removed package | Canonical package |
| --- | --- |
| `retryhttp` | `adapters/http` |
| `retrypgx` | `adapters/postgres` |
| `retrylog` | `adapters/slog` |
| `retrytelemetry` | `adapters/otel` |

Canonical HTTP errors use `NewError`, immutable `StatusCode()` and bounded
metadata. There is one Retry-After parser: raw input at most 128 bytes before
trimming, unsigned seconds with safe saturation, and standard HTTP dates.
`RetryAfterOptions`, `ParseRetryAfterWithOptions` and legacy admission knobs are
removed. Keep delay ceilings and positive-only domain decisions at consumers,
not as alternate parser grammars.

Shared work budgets use only `github.com/faustbrian/go-resilience/v2`. Migrate
all nested executors to the same scope owner; an old scope is not converted or
recognized. Retry releases acquired permits and borrows an already attached
current attempt without taking its completion ownership.

The OTel scope is `github.com/faustbrian/go-retry/v3/adapters/otel`. Update
Collector/dashboard/query selectors with consumers; do not dual-emit. Metric
names, units and bounded attributes remain unchanged. Restore application,
library, configuration and selectors together when rolling back.

The active `retryadapter` domain-predicate API remains. Its predicates classify
failures; they do not grant replay permission or take acknowledgement ownership.
