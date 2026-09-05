# Observability

Observers receive attempt number, elapsed time, selected next delay,
classification, and terminal reason. They never receive operation values or
error messages. Calling an observer directly propagates sink panics. `Do` and
`DoStrict` isolate observer panics so observation cannot replace an execution
result.

`adapters/slog` writes bounded slog fields. `adapters/otel` records count,
elapsed, and delay instruments through a caller-owned OpenTelemetry
`MeterProvider`. Policy IDs are limited to 128 bytes and must not contain
credentials, customer identifiers, URLs, SQL, or payload fragments. Unknown
outcomes use the exact bounded reason `outcome_unknown`; neither adapter emits
the operation value or arbitrary error text.

Direct slog and OpenTelemetry sink calls run synchronously with
`context.Background()`. They hold no package lock, so re-entry is allowed;
blocking blocks the caller, separate calls may invoke a sink concurrently, and
sink panics propagate to the direct caller. The adapters retain neither an
observation nor sink arguments after the call returns. Executing through `Do`
or `DoStrict` adds the engine's observer-panic isolation described above.

The successor OTel scope is
`github.com/faustbrian/go-retry/adapters/otel`. The compatibility
`retrytelemetry` package retains its original scope. The caller creates, owns,
flushes, and shuts down the logger or meter provider after all synchronous
observer calls finish.

Alert on exhaustion rate, elapsed-budget exhaustion, and sustained retry
volume. Do not alert on a single retry attempt without service context.
