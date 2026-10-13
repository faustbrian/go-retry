# Source documentation audit

[Documentation map](index.md) · [Repository README](../README.md)
[Next: Operations](operations.md)

## Scope and reproducibility

Tier A source-only pass from `0ffcc910712079149eba2b4caeb7ad26bc522bd5`.
Runtime, public signatures, dependency/fixture pins and frozen evidence stay
unchanged; the two optional formatting field terminators are explained below. The original unrelated checkout is not part of this change.
Reproduce: `git ls-files '*.go'`, `git ls-files '*go.mod'`,
`golib source-docs inventory --json`, `golib source-docs check --json`.
Go producer 1.27.2, module minimum 1.27.0; one module, ten public packages,
61 tracked Go files. There is no generated/vendor Go, platform variant, build
constraint, generator directive or cgo source in this inventory.

## Complete file and package inventory

| Directory | Package | Kind | Files |
| --- | --- | --- | ---: |
| `.` | `retry` | source | 8 |
| `.` | `retry` | test | 2 |
| `.` | `retry_test` | test | 21 |
| `adapters/http` | `retryhttp` | source | 2 |
| `adapters/http` | `retryhttp_test` | test | 3 |
| `adapters/otel` | `retryotel` | source | 2 |
| `adapters/otel` | `retryotel_test` | test | 2 |
| `adapters/postgres` | `retrypostgres` | source | 2 |
| `adapters/postgres` | `retrypostgres_test` | test | 1 |
| `adapters/slog` | `retryslog` | source | 2 |
| `adapters/slog` | `retryslog_test` | test | 1 |
| `retryadapter` | `retryadapter` | source | 1 |
| `retryadapter` | `retryadapter_test` | test | 1 |
| `retryhttp` | `retryhttp` | source | 1 |
| `retryhttp` | `retryhttp_test` | test | 3 |
| `retrylog` | `retrylog` | source | 1 |
| `retrylog` | `retrylog` | test | 1 |
| `retrylog` | `retrylog_test` | test | 1 |
| `retrypgx` | `retrypgx` | source | 1 |
| `retrypgx` | `retrypgx_test` | test | 1 |
| `retrytelemetry` | `retrytelemetry` | source | 1 |
| `retrytelemetry` | `retrytelemetry` | test | 1 |
| `retrytelemetry` | `retrytelemetry_test` | test | 2 |

```text
adapters/http/doc.go
adapters/http/fuzz_test.go
adapters/http/http.go
adapters/http/http_test.go
adapters/http/parser_boundary_test.go
adapters/otel/doc.go
adapters/otel/observer.go
adapters/otel/observer_test.go
adapters/otel/sdk_aggregation_test.go
adapters/postgres/classifier.go
adapters/postgres/classifier_test.go
adapters/postgres/doc.go
adapters/slog/doc.go
adapters/slog/observer.go
adapters/slog/observer_test.go
backoff.go
backoff_test.go
benchmark_test.go
contract_test.go
default_privacy_test.go
errors.go
example_test.go
execution.go
execution_edge_test.go
fuzz_test.go
jitter_statistical_test.go
leak_test.go
legacy_permit_test.go
mutation_boundaries_test.go
policy.go
policy_test.go
race_test.go
resilience_budget_test.go
resilience_v2_composition_test.go
retryadapter/classifier.go
retryadapter/classifier_test.go
retryhttp/default_admission_test.go
retryhttp/fuzz_test.go
retryhttp/http.go
retryhttp/http_test.go
retrylog/observer.go
retrylog/observer_test.go
retrylog/reason_internal_test.go
retrypgx/classifier.go
retrypgx/classifier_test.go
retrytelemetry/observer.go
retrytelemetry/observer_test.go
retrytelemetry/reason_internal_test.go
retrytelemetry/strict_test.go
strict_callbacks_test.go
strict_collaborator_behavior_test.go
strict_errors.go
strict_execution.go
strict_execution_internal_test.go
strict_execution_outcome_test.go
strict_policy.go
strict_policy_test.go
strict_terminal_error_test.go
successor_contract_test.go
system.go
system_test.go
```

Every file's declarations, private helpers and tests are included. Private
uppercase fixture members are not public API gaps. Root and four successor
adapters have canonical package comments; flat compatibility packages retain
package deprecations. retryadapter is supported, not deprecated.

## Missing documentation versus unanswered rationale

Initial source check: 70 missing public interface/field/method comments in
policy, execution, errors, strict outcomes, backoff and adapter options.
Existing comments omit borrowed collaborator lifetime, synchronous/blocking
callback behavior, error-object retention, constructor readiness, conditional
concurrency, optional-budget defaults and the two cancellation/outcome variants.
Add useful adjacent rationale for permit transfer, cancellation-before-release,
shared-budget version ownership, history truncation, saturating arithmetic,
RNG normalization, strict redacted carriers and transport/SQL precedence.

Actual contracts: MaxAttempts includes the original invocation; MaxSleep
accounts selected successful sleep requests, not measured sleeper wall time.
MaxElapsed is sampled with injected Clock and supplies cooperative attempt
contexts; no arbitrary collaborator can be preempted. Do discards the value
on caller cancellation after a callback, whereas DoStrict retains a known
success and never repeats an unknown outcome. A callback returning
OutcomeNotDispatched is invalid because dispatch has occurred. Config scalar
state is copied but collaborator/captured state is borrowed. Acquired permits
are completed, already attached attempts remain outer-owned; completion
errors are ignored and completion panics can propagate. Error history is
bounded by count, not retained error-object size. Legacy Do can return History
sharing a terminal error's stored slice; accessor snapshots copy that current
slice but not error objects. DoStrict stores a separate terminal slice.
Observations are not emitted for every possible terminal path and may report attempt zero on early work
admission denial. OTEL counter updates count observation events, not verified
physical operation dispatches.

Questions, not invented policies: exact budget/seed selection belongs to the
consumer; no repository evidence explains those choices. NaN centered jitter
factors have no specified useful bound or distribution; they are not validated. The endpoint exception for a full int64 RNG span and distinct legacy
SQLSTATE/cancellation and known-outcome precedence are retained behavior, not
new protocol conformance. Retry eligibility never establishes replay safety.

## Tests, markers, variants and native generation

All tests are inventoried: deterministic vectors, statistical seeded jitter,
exact budget bounds, canceled timer cleanup, concurrent/reentrant collaborator
seams, permit panic/cancellation exits, strict unknown-outcome refusal and
redacted cause carriers. Adapter tests cover bounded raw HTTP metadata,
SQLSTATE/context precedence, optional provider validation, synchronous sinks,
exact metric units/values and SDK-filtered aggregation. Existing fixtures and
peer benchmarks are not conformance or performance qualification here.
Two runnable root Examples show strict known outcomes and injected full jitter.
Five fuzz identities cover backoff, contradictory delay limits, HTTP parsing
in both variants and canonical HTTP response admission.

Four package deprecations are retained as idiomatic standalone paragraphs.
No unfinished-debt or generated-code marker occurs. Compiler/lint/security
suppression directives are unchanged. No generator-owned Go is rewritten.

Native .golib.yaml selects a full-prose go-doc baseline at api/baseline.txt.
Regenerate with native `go doc -all` in this exact configured package order:
`.`; `./adapters/http`; `./adapters/otel`; `./adapters/postgres`;
`./adapters/slog`; `./retryadapter`; `./retryhttp`; `./retrylog`;
`./retrypgx`; `./retrytelemetry`. Concatenate native output and normalize the
final trailing newline to one, repeating for stability. No new snapshot header
is invented. Native verification/package.mk docs selects root Examples and
GoDoc; no broader runtime/lifecycle campaign is required for unchanged behavior.

## Verification and resource state

Pinned Go 1.27.2 with GOWORK=off, GOMAXPROCS=2 and
GOFLAGS=-p=2 -mod=readonly, using owned GOCACHE/GOMODCACHE/GOTMPDIR:

- Native gofmt inspection: all 61 tracked Go files clean.
- Native comment-free ASTs, exported signatures, literals and compiler/lint/
  security directives: unchanged across all 61 files.
- The first strict raw-token check failed on errors.go: idiomatic field GoDoc
  expanded the two one-line Cause structs and introduced two optional implicit
  field terminators before closing braces. The failed session was cleaned.
  The corrected proof accounted only those two optional terminators alongside
  exact AST identity; all other tokens, including implicit semicolons, match.
  This is formatting, not an expression or behavior change, and raw-token
  byte identity is not claimed for those two declarations.
- Native `go doc -all` in the configured ten-package order: all rendered and
  inspected. api/baseline.txt regenerated twice stably using the native Tools
  final-newline recipe; final snapshot is 43,836 bytes. Final prose refinements
  were rerendered and the complete ordered snapshot repeated for stability.
- Native `go test -run=^Example -count=1 -timeout=2m ./...`: both root Examples
  passed. Nine adapter packages had no Examples; their runtime tests were not
  executed. Unchanged example/executable/dependency inputs justify reusing
  these results after final prose refinements, not a new runtime claim.
- Coordinator-owned `golib source-docs check --json`: 61 files, zero findings.
- Local Markdown file targets: 45 inspected, none missing. Package deprecations,
  generator absence and original dependency/config/fixture/frozen inputs are
  preserved. The API prose baseline is the required regenerated exception.

Final pinned Gitleaks v8.30.1 added-content/new-file scan (allow suppression
disabled, owned empty ignore file): zero findings across the 31 changed files.
One ordinary complete-diff review covered source, maintained prose and the
native generated API snapshot; no unresolved prose findings remain.
Both exact Go sessions' three caches, native helpers and rendered temporary
outputs are removed, with absence verified. The configured API baseline is a
maintained repository artifact, not leftover temporary output. Shared tooling
and the original checkout remain untouched; Go slot handed to the next worker.

No active fuzz, race/leak/lifecycle, peer/performance, public-consumer, release,
service or full test campaign was run. Example execution includes the existing
root TestMain hook; it is not broad runtime qualification. Parent owns exact-
source hosted CI and publication. There is no runtime repair or new replay,
privacy-erasure, distribution or hard-deadline guarantee.

## Objective Enforcement

The checksum-pinned public Tools v2.4.0 verifier now supplies
`make source-docs-check` as a prerequisite of the existing local and CI Make
routes. Ordinary shared CI selects strict source-documentation checking and
requires its result. Native prerequisites, operation budgets, security and
release controls are retained. Hosted exact-head evidence is recorded by the
coordination ledger; configuration alone does not establish green delivery.
