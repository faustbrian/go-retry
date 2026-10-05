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

Published v1 retains its historical imports and legacy cancellation, terminal
formatting, permissive HTTP configuration, PostgreSQL precedence, and OTel
scope behavior during the documented compatibility interval.

The v2 module uses `/v2` imports and retains the named compatibility packages,
not all v1 behavior. Flat HTTP constructors return admission errors and enforce
canonical input bounds; default terminal text does not format causes. Legacy
dispatch, PostgreSQL precedence, and OTel scope remain intentionally distinct
from successor variants. Follow [migration guidance](docs/migration.md) when
adopting v2. Successor named types have different reflection identities and
must not be treated as aliases.
