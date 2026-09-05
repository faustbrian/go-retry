# Deprecation Policy

Deprecations MUST identify the replacement, reason, migration steps, and
earliest removal version. Public Go identifiers use a valid `Deprecated:` doc
paragraph and corresponding changelog entry.

At `v1` and later, a supported replacement SHOULD exist for at least one minor
release before removal. Security or correctness defects MAY require faster
removal when continued support would be unsafe; the release notes must explain
the exception.

Silent behavior changes, undocumented aliases, and indefinite deprecated code
are prohibited. Deprecations are checked during compatibility and release
review.

## Target-oriented adapter migration

The package paths `retryhttp`, `retrypgx`, `retrylog`, and `retrytelemetry` are
deprecated in favor of `adapters/http`, `adapters/postgres`, `adapters/slog`,
and `adapters/otel`. The successors add strict validation, bounded disclosure,
or target-oriented observability identity without changing the legacy paths.
See [`docs/migration.md`](docs/migration.md) for exact behavior and type-identity
differences.

Removal cannot occur before the longer of 180 days after the successors become
publicly consumable or two stable minor root-module releases that contain both
old and new paths. It also requires owned-consumer migration, clean external
consumer evidence, and a separately authorized major release. Rollback consists
of restoring the corresponding legacy import and its documented v1 behavior.

`retryadapter` is not deprecated. Its caller-owned predicates are a justified
semantic boundary rather than naming debt.
