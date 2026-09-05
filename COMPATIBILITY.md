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

The additive strict root APIs and target-oriented adapter packages preserve all
released imports. Legacy cancellation, terminal formatting, permissive HTTP
configuration, PostgreSQL precedence, and OTel scope behavior remain available
during the compatibility interval. Successor named types deliberately have
different reflection identities and must not be treated as aliases.
