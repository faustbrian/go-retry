# Verification

[Documentation map](index.md) · [Repository README](../README.md)
[Next: Operations](operations.md)

Ordinary development starts with `go test ./...` or a focused test selector.
`make check` and `make ci` use the public verifier declared in `.golib.yaml`
for bounded local checks, including one named race selection for legacy/strict
concurrent execution, cancellation-time permit completion and sleeper lifecycle.
Documentation changes need relevant structure, links and examples, not unrelated
runtime work. Applicable security checks and fail-closed required results remain
enforced.

`make check-full` retains the explicit full gate: formatting, module
tidiness, vet, architecture boundaries, unit tests, race safety, exact 100%
production statement coverage, deterministic fuzz smoke, mutation checks,
leak detection, comparative benchmarks, documentation, public API, workflow
syntax, lint, Staticcheck, and vulnerability analysis.

Mutation is optional developer-local or selected scheduled assurance, not a PR
prerequisite; moving a campaign off PRs does not waive a confirmed defect. See
[contributor verification](../CONTRIBUTING.md#verification) for focused 2s fuzzing,
optional one-package mutation and selected API/adapter/composition requirements.
Original configured full fuzz budgets and repeated lifecycle operations remain
unchanged.

The deterministic strategy vectors include overflow and saturation. Jitter
tests use seeded sources and fixed statistical tolerances. Cancellation tests
cover before-attempt, in-attempt, and in-sleep precedence. HTTP tests cover
seconds, dates, clock skew, malformed input, and oversized values.

Relevant release qualification uses `golib release dry-run --module .` and
requires applicable exact-source hosted checks and public-consumer evidence.
A bounded local pass is not release qualification, and CI-only changes do not
require a module release.
