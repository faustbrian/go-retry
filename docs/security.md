# Security model: v3 API family

[Documentation map](index.md) · [Repository README](../README.md)
[Next: Operations](operations.md)

This model describes the v3 root and five adapter/domain packages. Publication
and runtime acceptance require separate evidence; this text does not certify them.

## Assets and boundaries

The protected assets are caller cancellation, retry/work-budget capacity,
attempt-context resources, operation outcomes, and private error/HTTP metadata.
The package performs no implicit network or filesystem access and starts no
background worker. Operations, classifiers, clocks, sleepers, backoff, random
sources, observers and optional work-budget scopes are application-owned
collaborators. Calls are synchronous and no retry lock spans a collaborator.

Policies copy scalar configuration. `MaxAttempts` includes the original
dispatch; elapsed, per-attempt, delay and total-sleep limits are explicit.
History is capped at 1024 entries. Built-in arithmetic saturates instead of
overflowing; polynomial/exponential evaluation uses logarithmic work and
Fibonacci evaluation caps its iteration count. A finite attempt count alone
does not preempt a blocked operation. The configured clock defines elapsed
accounting, while operation deadlines depend on cooperative context handling.

`Do` owns only permits it acquires. An outer physical
attempt already attached to context remains outer-owned. Acquired permits are
completed once after dispatch or on an exit that prevents dispatch; each
derived attempt context is canceled at the end of that attempt, including
panic unwinding. Completion errors are ignored; operation and completion
panics follow standard Go unwinding rather than recovery. This is resource
cleanup, not recovery of a dispatched side effect.

`Do` distinguishes known results from unknown outcomes and never retries
an unknown outcome. Its human-readable terminal errors are bounded and custom
context messages are normalized. Machine cause traversal and bounded history
may retain known error objects; callers must not treat those as safe log text.
Terminal metadata copies its history slice, but retains borrowed known error
objects. Markers and terminal errors use bounded categories without formatting
application causes. Machine cause traversal is an explicit trusted
diagnostic surface, not permission to log private causes.

The HTTP constructor owns canonical admission, validate configured statuses and retain at most
128 bytes of `Retry-After`, never response bodies, requests, URLs or other
headers. Its status text does not format the cause. HTTP/SQL classifiers decide
retry eligibility, not authorization or replay safety. Canonical PostgreSQL
classification gives caller cancellation precedence. Logging and telemetry
adapters receive bounded lifecycle fields rather than operation values.

## Conditional residual ownership

These are deployment/caller contracts, not blanket acceptance of defects in
the owned cleanup, bounds, or redaction paths.

| Boundary | Owner | Rationale | Mitigation | Review condition |
| --- | --- | --- | --- | --- |
| Synchronous collaborators, context/error traversal and callback panics | Application collaborator implementer | The API borrows caller implementations; it cannot forcibly preempt arbitrary Go calls. Most panics propagate; classifier and observer containment follow their documented variants. | Use bounded, context-cooperative implementations, synchronize shared state, keep backing resources alive until all calls return, and avoid cyclic/custom blocking error graphs. | Reassess when exposing collaborator configuration to untrusted callers or adding asynchronous execution. |
| Retry count, duration policy and custom time/backoff sources | Application policy owner | Finite bounds are explicit trusted configuration, not a package-wide workload quota. A caller can select costly counts or blocking collaborators. | Set conservative count/elapsed/attempt/sleep bounds and apply admission before accepting request-derived policies. Use the shared work scope for nested amplification. | Reassess when configuration becomes remote-controlled or workload/concurrency limits change. |
| Replay, ambiguous outcomes and authorization | Operation/application owner | A retry classifier cannot prove idempotency or undo effects. A process-local optional budget is not distributed admission. | Use idempotency keys or transaction/provider reconciliation, stop on `ErrOutcomeUnknown`, and apply authorization and cross-process admission outside retry. | Review each new operation, remote provider or nested retry/hedge composition. |
| Explicit machine causes/history | Application disclosure owner | Machine identities may contain application-private data; entry-count limits do not bound error-object size. | Keep history small or disabled, and classify/redact before logging or traversing retained causes. Human error text must not be replaced with raw cause text. | Review on diagnostic export or a change to error ownership/retention. |
| Logger, meter provider, transient predicate and shared scope lifecycle | Application integration owner | Optional adapters borrow providers/callbacks; retry does not own their shutdown or distributed capacity policy. | Configure bounded telemetry attributes, nonblocking/bounded handlers and predicates, and close providers/scopes only after all dependent calls finish. | Review when replacing an integration provider or changing process/distributed ownership. |

## Verification and release verdict

Focused public regressions cover cancellation after original and retry
admission, elapsed-budget pre-dispatch exit, and panic cleanup, with exact
permit/context ownership and existing classification preserved. Outcome and redaction controls belong to the sole executor.
Source review, affected tests/static/API checks and required exact-source CI
are necessary evidence for this major. A public release additionally requires
its existing release rehearsal, trusted release signatures and artifacts,
and a clean public consumer. This model alone certifies none of those delivery states.
