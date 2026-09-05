# Migration

1. Inventory every existing loop and the exact failures it retries.
2. Prove repeat safety independently of error transience.
3. Convert implicit retry rules into an explicit classifier.
4. Choose a strategy and calculate attempt, elapsed, per-attempt, delay, and
   sleep bounds.
5. Inject clock, sleeper, random source, and observer dependencies.
6. Compare old and new attempt counts and terminal causes in shadow metrics.
7. Remove nested library retries or include them in the total budget.

When migrating from cenkalti/backoff or avast/retry-go, note that `retry`
rejects zero attempts and never supplies an implicit classifier or policy.

## Strict execution migration

Use `NewPolicyStrict` and `DoStrict` for new operations. The callback returns
`AttemptResult[T]` and must declare `OutcomeKnown` only when its value or error
conclusively describes the attempt. Return `OutcomeUnknown` with a non-nil
error when dispatch happened but the side effect cannot be proved. The engine
does not retry that result; reconcile it before any replay. Callbacks must not
return `OutcomeNotDispatched`.

The strict surface intentionally differs from `NewPolicy`, `Do`, and
`CanceledError`:

- typed-nil optional random and observer implementations are rejected;
- known callback results win a racing caller cancellation;
- cancellation and deadline errors contain only normalized context sentinels;
- unknown outcomes expose `ErrOutcomeUnknown` and bounded text;
- known terminal wrappers keep machine cause traversal but use bounded safe
  human strings; and
- classifier panics become `ErrInvalidPolicy` with no panic or operation cause
  disclosure.

Known terminal failures use the following strict matrix. Text is exact and
bounded; terminal-wrapper causes remain available through
`errors.Is`/`errors.As` without entering human text. Positive-history rows
separately retain the exact operation error in `StrictResult.Retry.History`.

| Boundary | Wrapper and exact text | Cause traversal | Positive-history rule |
| --- | --- | --- | --- |
| permanent classification | `*PermanentError`: `permanent: retry operation failed` | operation | current permanent attempt |
| attempts exhausted | `*ExhaustedError`: `retry attempts exhausted: retry operation failed` | operation | current retryable attempt |
| classifier error | `*PermanentError`: `permanent: retry classifier failed` | operation, then classifier | no current attempt |
| invalid classification | `*PermanentError`: `permanent: retry classifier failed` | operation | no current attempt |
| sleeper failure | `*PermanentError`: `permanent: retry sleeper failed` | sleeper | already-recorded retryable operation attempt; no second sleeper entry |
| elapsed after failure | `*BudgetError`/`BudgetElapsed`: `retry elapsed budget exhausted: retry operation failed` | operation | current retryable attempt and selected delay |
| sleep after failure | `*BudgetError`/`BudgetSleep`: `retry sleep budget exhausted: retry operation failed` | operation | current retryable attempt and selected delay |
| attempt timeout | `*BudgetError`/`BudgetAttempt`: `retry attempt budget exhausted: retry attempt timed out` | operation, then `context.DeadlineExceeded` | no current attempt |
| elapsed-bounded attempt timeout | `*BudgetError`/`BudgetElapsed`: `retry elapsed budget exhausted: retry attempt timed out` | operation, then `context.DeadlineExceeded` | no current attempt |
| elapsed before/between attempts | `*BudgetError`/`BudgetElapsed`: `retry elapsed budget exhausted: retry deadline reached` | `context.DeadlineExceeded` | none before dispatch; only earlier attempts between dispatches |
| work admission | `*BudgetError`/`BudgetWork`: `retry work budget exhausted: retry work admission failed` | admission cause | none initially; only earlier attempts after dispatch |

With `HistoryLimit` zero, every row has empty history without changing terminal
cause traversal. `BudgetError.Result` and `ExhaustedError.Result` return
defensive snapshots. A strict classifier panic instead returns exactly
`invalid retry policy: classifier panicked`, matches `ErrInvalidPolicy`, and
retains neither the operation cause nor panic value. Legacy `Do` continues to
format the panic and join the operation and classifier errors.

The v1 legacy functions retain their original precedence, cause formatting,
typed-nil normalization, and classifier-panic disclosure. Roll back by using
those legacy functions while preserving the operation's own replay decision.

## Adapter migration

| Compatibility path | Preferred path | Intentional difference |
| --- | --- | --- |
| `retryhttp` | `adapters/http` | validates status sets, bounds `Retry-After`, and never formats the response cause |
| `retrypgx` | `adapters/postgres` | active caller context wins globally; returned context sentinels win over helper, EOF, and network evidence but not SQLSTATEs |
| `retrylog` | `adapters/slog` | target-oriented package identity and total nil/zero observer behavior |
| `retrytelemetry` | `adapters/otel` | typed-nil provider rejection, target-oriented identity, and successor import-path scope |

Successor exported types are distinct named types; reflection and type switches
observe their new package paths. The OTel scope changes from
`github.com/faustbrian/go-retry/retrytelemetry` to
`github.com/faustbrian/go-retry/adapters/otel`. If the old scope is part of a
dashboard or alert, migrate that query before switching. Callers temporarily
remaining on `retrytelemetry` can use `NewStrict` for typed-nil validation while
retaining the legacy scope.

Roll back adapter migrations independently: change `adapters/http` imports to
`retryhttp`, `adapters/postgres` to `retrypgx`, `adapters/slog` to `retrylog`,
and `adapters/otel` to `retrytelemetry`. Restore the corresponding legacy
constructor and named types at the same time. HTTP rollback restores permissive
status and cause-formatting behavior; PostgreSQL rollback restores SQLSTATE-first
caller-context behavior. Before an OTel rollback, restore dashboards and alerts
from the successor scope to the legacy `retrytelemetry` scope. Keep strict root
execution independent of an adapter rollback unless its outcome semantics are
also being deliberately reverted.

The old and new paths remain supported together for the longer of 180 days
after public successor availability or two stable minor releases containing
both. External consumer population is not fully known, so removal additionally
requires a separately reviewed major release and clean consumer evidence.
These packages share the root module version; install the root module rather
than expecting adapter-specific tags.

`retryadapter` remains a supported generic predicate adapter and is not being
renamed. Its queue, webhook, filesystem, and object-storage classifiers own no
target dependency or replay decision. The root v1 module also retains its pgx
and OpenTelemetry dependency bundle pending a separate extraction decision.

Phase 3 does not select a universal order for retry, rate limit, breaker,
bulkhead, concurrency limit, hedge, adaptive throttle, or timeout. That order
is deferred to the Phase 4 composition contract. Migrating an import must not
silently change the application's policy order.

## Residual compatibility register

- The root v1 module continues to bundle pgx and OpenTelemetry dependencies.
- `retryadapter` remains supported and is not deprecated or split.
- Legacy `retryhttp` keeps permissive status filtering, duplicate acceptance,
  unbounded `Retry-After` input, and cause-bearing human error formatting.
- Legacy `retrypgx` keeps PostgreSQL-first classification precedence even when
  caller cancellation is already active.
- Legacy `NewPolicy`, `Do`, and observer adapters keep their documented
  typed-nil, cancellation-race, nil-receiver, and terminal formatting behavior.
  In particular, legacy known-terminal and classifier-panic errors may be
  unbounded and disclose or directly traverse original causes.
- External consumer population remains unverified beyond the recorded owned
  consumer inventory.
- Resilience-stack ordering remains deferred to the Phase 4 composition
  contract.
