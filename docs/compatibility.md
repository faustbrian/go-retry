# Compatibility

The v2.0.0 module uses `github.com/faustbrian/go-retry/v2` and requires
Go 1.27.0. Published v1.1.0 uses Go 1.26.6 and remains an immutable historical
release. v2 publication is established only by public tags and releases.
Root markers and terminal errors no longer format causes; explicit `Unwrap`
and result history retain trusted machine diagnostics. Flat HTTP constructors
now return errors rather than silently admitting invalid or over-limit input.
See [migration guidance](migration.md) for call-site changes.

The module targets Go 1.27.0 and follows Go module semantic-versioning rules.
The root package is dependency-light; pgx and OpenTelemetry dependencies enter
only through adapter packages.

Public API compatibility is recorded in `api/baseline.txt`. Intentional public
changes require a changelog entry and regenerated baseline. Major releases may
still change API, but migration guidance must accompany breaking changes.

The target-oriented `adapters/http`, `adapters/postgres`, `adapters/slog`, and
`adapters/otel` packages are additive root-module packages. They are not
independently versioned. The released `retryhttp`, `retrypgx`, `retrylog`, and
`retrytelemetry` paths remain supported for the longer of 180 days after the
successors are publicly consumable or two stable minor root-module releases
containing both paths. Time alone cannot end that interval.

Successor named types intentionally have their successor package reflection
identity. The OTel successor uses its own import path as scope. The root
module still bundles pgx and OpenTelemetry dependencies; extracting them
requires a separate reviewed module-boundary decision.
