# Compatibility Policy

The repository is one releasable Go module and follows semantic versioning.
Root tags use `v<version>`. Public `adapters/*` directories are packages in
that root module and do not receive independent tags.

Before `v1`, minor releases MAY contain reviewed breaking changes, but every
break MUST be documented with migration guidance. Patch releases MUST remain
backward compatible. At and after `v1`, incompatible exported API or documented
behavior changes require a new major version.

Compatibility includes exported Go APIs, error classification, serialization,
protocol behavior, persistence schemas, environment variables, command output,
resource ownership, ordering, retry/idempotency semantics, and documented
defaults. A compile-compatible change can still be behaviorally breaking.

Specification-backed modules MUST NOT diverge from their declared standards.
Ambiguities require documented decisions and stable tests. Deprecated APIs
follow [`DEPRECATION.md`](DEPRECATION.md).

Published historical versions remain immutable and retain their own contracts.
Version 3 uses `/v3` imports, one explicit-outcome executor and canonical adapter
packages. It removes obsolete APIs and bridging paths without aliases. Follow
[migration guidance](docs/migration.md) and qualify each actual consumer.
