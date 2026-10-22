# Budgets and cancellation

[Documentation map](index.md) · [Repository README](../README.md)
[Next: Operations](operations.md)

`MaxAttempts` is mandatory and includes the first call. `MaxElapsed` samples
elapsed time through Clock and supplies a remaining
cooperative attempt deadline; it is not forced callback preemption.
`AttemptTimeout` supplies a cooperative deadline to each operation. `MaxSleep`
bounds accumulated successful requested delays, not measured sleeper wall time.
Sleeper receives the caller context, not a separate elapsed-budget context.
`MinDelay` and `MaxDelay` clamp each selected delay, including Retry-After hints;
the maximum can shorten a server hint. Zero duration budgets disable that bound.

With `Do`, cancellation before dispatch returns `ContextError` with
`OutcomeNotDispatched`; cancellation between known attempts returns
`OutcomeKnown`; and cancellation after ambiguous dispatch returns
`OutcomeUnknown`. A complete known callback result wins a cancellation race.
Context errors expose only normalized `context.Canceled` or
`context.DeadlineExceeded`, never a custom cancellation or sleeper message.
Policy elapsed or attempt deadlines return bounded `BudgetError` values. Delay
that cannot fit the remaining elapsed or sleep budget is rejected before
sleeping.

Use both attempt and elapsed budgets for remote calls. A finite attempt count
alone does not limit a blocked operation.

## Shared retry-plus-hedge work

Set `Config.UseResilienceBudget` to consume the `resilience.WorkBudgetScope`
attached to the call context. The first physical attempt is admitted as
original work unless an outer executor already placed an attempt in context.
Every later retry is admitted as additional work with explicit parent lineage.

Only `github.com/faustbrian/go-resilience/v2` is supported. Its scope, ordinal,
attempt context and permits remain authoritative throughout execution. Old
scope contexts are not converted; missing current scopes fail closed.
Callers own the logical scope and must close it after all nested work finishes.
Retry completes permits it acquires, but an outer executor retains completion
ownership of an already attached physical attempt.

Missing scope, closed scope, invalid lineage, or capacity denial stops before
the next downstream invocation with `ReasonWorkBudget` and a `BudgetError`
whose kind is `BudgetWork`. The error unwraps to the original resilience error,
so callers can distinguish `ErrBudgetRejected` from cancellation or dependency
failure through `errors.Is`.

The option is deliberately opt-in. A false value preserves the standalone
retry behavior and its elapsed, sleep, attempt, and count bounds. A shared
scope is process-local unless its selected `WorkBudget` implementation states
otherwise.
