# Compatibility

The module targets Go 1.26.5 and follows Go module semantic-versioning rules.
The root package is dependency-light; pgx and OpenTelemetry dependencies enter
only through adapter packages.

Public API compatibility is recorded in `api/baseline.txt`. Intentional public
changes require a changelog entry and regenerated baseline. Pre-v1 releases may
still change API, but migration guidance must accompany breaking changes.
