# Security policy

Report vulnerabilities privately to the repository owner. Do not include
credentials, production payloads, or customer errors in reports.

Retry storms can amplify incidents. Policies should use jitter, conservative
attempt and elapsed bounds, cancellation, and bounded telemetry attributes.
Callers remain responsible for idempotency, authorization, and side effects.

Prefer `NewPolicyStrict` and `DoStrict` for new work. Their terminal human
errors are bounded to 66 bytes, custom context causes are normalized, and an
ambiguous dispatched result becomes `ErrOutcomeUnknown` without retaining or
formatting the callback error. Known machine causes remain available through
explicit `errors.Is` and `errors.As` traversal. Do not log those causes without
applying the application's disclosure policy.

`adapters/http.NewError` retains no response body, request, URL, credentials,
or headers other than at most 128 bytes of `Retry-After`. Its `Error` string
contains only the validated status code. The legacy `retryhttp` error can
format its cause and is retained only as a compatibility surface.
