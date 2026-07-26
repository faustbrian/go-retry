# Observability

Observers receive attempt number, elapsed time, selected next delay,
classification, and terminal reason. They never receive operation values or
error messages. Observer panics are isolated from execution.

`retrylog` writes bounded slog fields compatible with `log`.
`retrytelemetry` records count, elapsed, and delay instruments through the
OpenTelemetry `MeterProvider` used by `telemetry`. Policy IDs are limited to
128 bytes and must not contain credentials, customer identifiers, URLs, SQL,
or payload fragments.

Alert on exhaustion rate, elapsed-budget exhaustion, and sustained retry
volume. Do not alert on a single retry attempt without service context.
