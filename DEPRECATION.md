# API Replacement Policy

A breaking replacement removes the superseded API in the new major version.
The release notes and migration guide must identify the replacement, reason,
observable behavior changes and consumer adoption steps. Do not introduce an
alias, forwarding package or parallel legacy engine to postpone migration.
Published versions and their history remain immutable; they are not part of
the current supported source surface.

## Version 3 adapter and executor replacement

Version 3 removes `retryhttp`, `retrypgx`, `retrylog` and `retrytelemetry`.
Their replacements are `adapters/http`, `adapters/postgres`, `adapters/slog`
and `adapters/otel`. It also replaces the two executor and policy-constructor
variants with one explicit-outcome `Do` and one validating `NewPolicy`.
Only the current Resilience v2 scope is supported. There is no coexistence
interval or compatibility bridge in this major version.

Migrate consumers, their tests and observability selectors together using
[`docs/migration.md`](docs/migration.md). Qualify the selected released module
and the complete logical execution before rollout. Rollback restores the
previous application, dependency and configuration revision together; it does
not reintroduce legacy imports into the version 3 application.

`retryadapter` is an active caller-owned predicate boundary for domain policy,
not a deprecated package or a second retry engine.
