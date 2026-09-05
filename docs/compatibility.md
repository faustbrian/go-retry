# Compatibility

The module targets Go 1.26.6 and follows Go module semantic-versioning rules.
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
identity. The OTel successor uses its own import path as scope. The root v1
module still bundles pgx and OpenTelemetry dependencies; extracting them
requires a separate reviewed module-boundary decision.
