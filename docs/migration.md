# Migration

## v1 to the v2 module

v2 requires Go 1.27.0 and imports `github.com/faustbrian/go-retry/v2`.
Update all ten package imports together; no version-specific source directory
or branch exists. Published v1.1.0 remains immutable and uses Go 1.26.6.
Adopt v2.0.0 only after its public tag and module are available.

Root `RetryableError`, `PermanentError`, `ExhaustedError`, `CanceledError`, and
`BudgetError` now use safe category text without invoking application causes.
Use type/sentinel classification and `Result.Reason`, not old error strings.
Explicit `Unwrap`, `errors.Is`/`errors.As`, and history remain trusted diagnostic
surfaces: apply disclosure policy before logging their contents. v1 and v2
sentinel values are distinct; use the sentinel from the selected major.
`Do` retains its dispatch and cancellation precedence; `DoStrict` remains the
explicit outcome-aware alternative, not an automatic replacement.

Flat `retryhttp.NewClassifier` now returns `(*Classifier, error)` and
`retryhttp.StatusError` returns `(*Error, error)`. Handle constructor errors
before execution. Both reject invalid statuses, duplicate/over-900 status sets,
and headers over 128 bytes through canonical `adapters/http` admission, with
no truncation or silent filtering. Nil status selection means defaults; an
explicit empty slice means no response statuses retry. Flat types remain
distinct from successor types, and `Error.StatusCode` remains an integer field.
Invalid literal/mutated status fields classify permanent without calling the
transport predicate; the zero-value flat classifier remains permanent.
Non-flat errors, including successor HTTP errors, still go to the flat
`Transient` predicate; the flat classifier recognizes only its own named error
type for status lookup and continues to ignore caller-context cancellation.
Both public `ParseRetryAfter` functions reject raw strings over 128 bytes
before trimming or parsing; the exact 128-byte boundary remains inclusive.

A classifier panic in `Do` becomes a safe classifier-failure category. The
operation cause and an error-valued panic remain available only through
explicit machine traversal; non-error panic values are not formatted or
retained. Strict classifier panic normalization is unchanged. Operation and
permit-completion panics still propagate with their original identity.

## General retry adoption

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
| permanent classification | `*PermanentError`: `permanent error` | operation | current permanent attempt |
| attempts exhausted | `*ExhaustedError`: `retry attempts exhausted` | operation | current retryable attempt |
| classifier error | `*PermanentError`: `permanent error` | operation, then classifier | no current attempt |
| invalid classification | `*PermanentError`: `permanent error` | operation | no current attempt |
| sleeper failure | `*PermanentError`: `permanent error` | sleeper | already-recorded retryable operation attempt; no second sleeper entry |
| elapsed after failure | `*BudgetError`/`BudgetElapsed`: `retry elapsed budget exhausted` | operation | current retryable attempt and selected delay |
| sleep after failure | `*BudgetError`/`BudgetSleep`: `retry sleep budget exhausted` | operation | current retryable attempt and selected delay |
| attempt timeout | `*BudgetError`/`BudgetAttempt`: `retry attempt budget exhausted` | operation, then `context.DeadlineExceeded` | no current attempt |
| elapsed-bounded attempt timeout | `*BudgetError`/`BudgetElapsed`: `retry elapsed budget exhausted` | operation, then `context.DeadlineExceeded` | no current attempt |
| elapsed before/between attempts | `*BudgetError`/`BudgetElapsed`: `retry elapsed budget exhausted` | `context.DeadlineExceeded` | none before dispatch; only earlier attempts between dispatches |
| work admission | `*BudgetError`/`BudgetWork`: `retry work budget exhausted` | admission cause | none initially; only earlier attempts after dispatch |

With `HistoryLimit` zero, every row has empty history without changing terminal
cause traversal. `BudgetError.Result` and `ExhaustedError.Result` return
defensive snapshots. A strict classifier panic instead returns exactly
`invalid retry policy: classifier panicked`, matches `ErrInvalidPolicy`, and
retains neither the operation cause nor panic value. `Do` joins operation and safe classifier errors without formatting a panic.

Published v1 legacy functions retain their historical precedence, cause
formatting, typed-nil normalization and classifier-panic disclosure. Rolling
back the major requires restoring v1 imports and its explicitly different
privacy/compiler contract, not merely selecting `Do` inside v2.

## Adapter migration

| Compatibility path | Preferred path | Intentional difference |
| --- | --- | --- |
| `retryhttp` | `adapters/http` | both use canonical admission in v2; successor uses immutable status accessor and its distinct named types |
| `retrypgx` | `adapters/postgres` | active caller context wins globally; returned context sentinels win over helper, EOF, and network evidence but not SQLSTATEs |
| `retrylog` | `adapters/slog` | target-oriented package identity and total nil/zero observer behavior |
| `retrytelemetry` | `adapters/otel` | typed-nil provider rejection, target-oriented identity, and successor import-path scope |

Successor exported types are distinct named types; reflection and type switches
observe their new package paths. The OTel scope changes from
`github.com/faustbrian/go-retry/retrytelemetry` to
`github.com/faustbrian/go-retry/v2/adapters/otel`. If the old scope is part of a
dashboard or alert, migrate that query before switching. Callers temporarily
remaining on `retrytelemetry` can use `NewStrict` for typed-nil validation while
retaining the legacy scope.

The Go module upgrade alone does not change the flat adapter's released scope.
For callers already using the v1 successor, the major upgrade changes its scope
from `github.com/faustbrian/go-retry/adapters/otel` to
`github.com/faustbrian/go-retry/v2/adapters/otel`; update scope-filtered dashboards
and alerts before that upgrade.

Roll back adapter migrations independently: change `adapters/http` imports to
`retryhttp`, `adapters/postgres` to `retrypgx`, `adapters/slog` to `retrylog`,
and `adapters/otel` to `retrytelemetry`. Restore the corresponding legacy
constructor and named types at the same time. Within v2, HTTP rollback preserves canonical admission and safe cause text;
PostgreSQL rollback restores SQLSTATE-first caller-context behavior. Before an OTel rollback, restore dashboards and alerts
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
target dependency or replay decision. The root module also retains its pgx
and OpenTelemetry dependency bundle pending a separate extraction decision.

Phase 3 does not select a universal order for retry, rate limit, breaker,
bulkhead, concurrency limit, hedge, adaptive throttle, or timeout. That order
is deferred to the Phase 4 composition contract. Migrating an import must not
silently change the application's policy order.

## Residual compatibility register

- The root module continues to bundle pgx and OpenTelemetry dependencies.
- `retryadapter` remains supported and is not deprecated or split.
- Flat `retryhttp` keeps its named type identities and public status field,
  but v2 constructors reject invalid input through shared canonical admission.
- Legacy `retrypgx` keeps PostgreSQL-first classification precedence even when
  caller cancellation is already active.
- Retained `NewPolicy`, `Do`, and observer adapters keep their documented
  typed-nil, cancellation-race and nil-receiver behavior. v2 default terminal
  text is safe; machine causes remain explicitly trusted. Published v1
  known-terminal and classifier-panic messages retain historical disclosure.
- External consumer population remains unverified beyond the recorded owned
  consumer inventory.
- Resilience-stack ordering remains deferred to the Phase 4 composition
  contract.
