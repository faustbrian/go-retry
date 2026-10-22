# Compatibility

[Documentation map](index.md) · [Repository README](../README.md)
[Next: HTTP and PostgreSQL adapters](adapters.md)

Version 3 uses `github.com/faustbrian/go-retry/v3` and requires Go 1.27.0.
Select a patched compiler for security-qualified builds. The module's declared
`go.mod` and `go.sum` select dependencies; historical inventories are not pins.

This major deliberately removes the duplicate executor/constructor generation,
Resilience v1 support, deprecated flat adapters and legacy parser options.
Follow the [migration guide](migration.md); no forwarding or alias interval is
provided in v3. Published earlier versions and applied data are not rewritten.

The root package uses current Resilience through an opt-in work-budget contract.
pgx and OpenTelemetry code enter only through adapters, although the root module
bundles their dependencies. No module extraction or global resilience stack is
implied by this change.

`api/baseline-v3.txt` records the current public API. Generating and checking a
new-major baseline establishes the v3 surface, not compatibility with v2.
Changes within v3 follow semantic versioning and require appropriate migration
and changelog entries. Native package/module checks and actual published consumer
compilation establish source composition, not production rollout.
