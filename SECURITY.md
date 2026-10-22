# Security policy

Report vulnerabilities privately to the repository owner. Do not include
credentials, production payloads, or customer errors in reports.

Retry storms can amplify incidents. Policies should use jitter, conservative
attempt and elapsed bounds, cancellation, and bounded telemetry attributes.
Callers remain responsible for idempotency, authorization, and side effects.

The versioned [security model](docs/security.md) describes the v3 execution,
privacy and resource boundaries. Tags, release integrity and consumer validation
establish publication, not source or documentation alone.

Use the sole `NewPolicy` and `Do` API. Terminal human errors are bounded to
66 bytes, custom context causes are normalized, and ambiguous dispatched results
stop as `ErrOutcomeUnknown` without retaining the callback error. Known machine
causes may remain reachable through `errors.Is`/`errors.As`; apply the application's
disclosure policy before traversing or exporting them.

`adapters/http.NewError` retains no body, request, URL, credentials or headers
other than at most 128 raw bytes of `Retry-After`. Status metadata is immutable
and error text does not format its cause. No flat mutable-status owner remains.
