# Security policy

Report vulnerabilities privately to the repository owner. Do not include
credentials, production payloads, or customer errors in reports.

Retry storms can amplify incidents. Policies should use jitter, conservative
attempt and elapsed bounds, cancellation, and bounded telemetry attributes.
Callers remain responsible for idempotency, authorization, and side effects.
