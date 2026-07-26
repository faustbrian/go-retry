# HTTP and PostgreSQL adapters

`retryhttp` recognizes 408, 425, 429, 500, 502, 503, and 504 by default. It
parses `Retry-After` delta-seconds and HTTP dates, saturates oversized values,
and retains only status, Retry-After, and the cause. Delay hints remain subject
to policy maximum delay and budgets. Transport classification is opt-in.

`retrypgx` recognizes SQLSTATE class `08`, serialization failure `40001`,
deadlock `40P01`, lock unavailable `55P03`, and selected server restart states.
Constraint violations, syntax errors, authentication failures, and query
cancellation remain permanent.

`retryadapter` requires caller predicates for queue, webhook, filesystem, and
object-storage failures. Those adapters deliberately know nothing about
acknowledgements, multipart completion, conditional writes, or idempotency.
