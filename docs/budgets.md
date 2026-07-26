# Budgets and cancellation

`MaxAttempts` is mandatory and includes the first call. `MaxElapsed` bounds
attempt work and sleeps. `AttemptTimeout` bounds each attempt. `MaxSleep`
bounds accumulated sleep, while `MinDelay` and `MaxDelay` clamp each selected
delay.

The earliest caller cancellation or deadline wins. A caller cancellation
returns `CanceledError`. A policy elapsed or attempt deadline returns
`BudgetError`. Delay that cannot fit the remaining elapsed or sleep budget is
rejected before sleeping. Context-aware sleepers make cancellation observable
during waits.

Use both attempt and elapsed budgets for remote calls. A finite attempt count
alone does not limit a blocked operation.
