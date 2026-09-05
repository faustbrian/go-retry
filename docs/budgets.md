# Budgets and cancellation

`MaxAttempts` is mandatory and includes the first call. `MaxElapsed` bounds
attempt work and sleeps. `AttemptTimeout` bounds each attempt. `MaxSleep`
bounds accumulated sleep, while `MinDelay` and `MaxDelay` clamp each selected
delay.

With `DoStrict`, cancellation before dispatch returns `ContextError` with
`OutcomeNotDispatched`; cancellation between known attempts returns
`OutcomeKnown`; and cancellation after ambiguous dispatch returns
`OutcomeUnknown`. A complete known callback result wins a cancellation race.
Context errors expose only normalized `context.Canceled` or
`context.DeadlineExceeded`, never a custom cancellation or sleeper message.
Policy elapsed or attempt deadlines return bounded `BudgetError` values. Delay
that cannot fit the remaining elapsed or sleep budget is rejected before
sleeping.

Legacy `Do` and `CanceledError` retain their v1 cancellation precedence and
cause formatting for compatibility.

Use both attempt and elapsed budgets for remote calls. A finite attempt count
alone does not limit a blocked operation.

## Shared retry-plus-hedge work

Set `Config.UseResilienceBudget` to consume the `resilience.WorkBudgetScope`
attached to the call context. The first physical attempt is admitted as
original work unless an outer executor already placed an attempt in context.
Every later retry is admitted as additional work with explicit parent lineage.

Missing scope, closed scope, invalid lineage, or capacity denial stops before
the next downstream invocation with `ReasonWorkBudget` and a `BudgetError`
whose kind is `BudgetWork`. The error unwraps to the original resilience error,
so callers can distinguish `ErrBudgetRejected` from cancellation or dependency
failure through `errors.Is`.

The option is deliberately opt-in. A false value preserves the standalone
retry behavior and its elapsed, sleep, attempt, and count bounds. A shared
scope is process-local unless its selected `WorkBudget` implementation states
otherwise.
