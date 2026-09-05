# HTTP and PostgreSQL adapters

`adapters/http` recognizes 408, 425, 429, 500, 502, 503, and 504 by default.
`New` rejects invalid or duplicate status configuration and copies the caller
slice. `NewError` retains only validated status, at most 128 bytes of the first
`Retry-After` value, and the exact machine-traversable cause. Its human error
text never includes that cause. Delay hints remain subject to policy maximum
delay and budgets. Transport classification is explicit and never establishes
replay safety.

`adapters/postgres` recognizes SQLSTATE class `08`, serialization failure `40001`,
deadlock `40P01`, lock unavailable `55P03`, selected server restart states,
pgx failures explicitly marked safe to retry, closed connections, transport
timeouts, truncated responses, and network failures. An active caller context
takes precedence over every backend classification. A returned error matching
cancellation or deadline takes precedence over pgx helper, EOF, and network
evidence, while a PostgreSQL SQLSTATE retains its legacy priority. Constraint
violations, syntax errors, authentication failures, and query cancellation
remain permanent.

`adapters/slog` and `adapters/otel` synchronously export bounded observations.
The caller owns logger handlers and meter providers for the observer lifetime.
The OTel instrumentation scope is the successor import path.

The released `retryhttp`, `retrypgx`, `retrylog`, and `retrytelemetry` packages
remain importable compatibility paths. See [Migration](migration.md) before
switching because successor types have distinct reflection identities and the
strict paths intentionally differ at validation and disclosure boundaries.

`retryadapter` requires caller predicates for queue, webhook, filesystem, and
object-storage failures. Those adapters deliberately know nothing about
acknowledgements, multipart completion, conditional writes, or idempotency.
It is a supported semantic adapter and is not deprecated or split merely for
directory-name uniformity.
