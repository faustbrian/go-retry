# Changelog

All notable changes use [Keep a Changelog](https://keepachangelog.com/) style.

## [Unreleased]

### Changed

- Select OpenTelemetry v1.47.0 API and metric SDK modules together,
  retaining exact retry measurements, filtered aggregation and distinct
  legacy and successor scopes with caller-owned provider lifetime.
  Applications using OTEL_GO_X_METRIC_EXPORT_BATCH_SIZE must configure
  sdkmetric.WithMaxExportBatchSize instead; retry does not own readers
  or exporters.

### Added

- Consume published Resilience/v2 work budgets alongside retained v1 budgets
  in both legacy and strict execution. A shared-budget call selects one scope
  version, preserves its attempt lineage and permit ownership, and refuses
  contexts carrying both versions before dispatch.

## [2.0.0] - 2026-10-05

### Changed

- Select pgx v5.11.0 while retaining PostgreSQL SQLSTATE and connection
  failure classification, including the distinct caller-cancellation
  precedence of legacy and successor classifiers.
- Select OpenTelemetry v1.46.0 API and metric SDK modules together, retaining
  exact retry measurements and caller-filtered aggregation across legacy,
  strict legacy, and successor observers with caller-owned provider lifetime.
- Select OpenTelemetry v1.45.0 API and metric SDK modules together, retaining
  retry metric names, bounded attributes, and the distinct legacy and target
  instrumentation scopes. The selected SDK also patches verbose internal
  trace-exporter diagnostics; retry's metric adapters do not create exporters,
  and provider lifetime and diagnostic configuration remain caller-owned.

- Move the module and its ten public packages to the required `/v2` import
  suffix and require Go 1.27.0. Source remains on main.
- Make default marking and terminal errors expose only safe categories while
  retaining explicit machine cause traversal and legacy dispatch ordering.
- Make flat HTTP constructors return admission errors for invalid status sets,
  status codes, and over-limit Retry-After metadata through the canonical HTTP
  owner; preserve distinct flat types and the public integer status field.
- Reject direct Retry-After parser inputs over 128 raw bytes before trimming
  or parsing, while retaining the inclusive boundary and accepted grammar.

### Fixed

- Complete legacy retry work permits when cancellation, an elapsed-budget exit,
  or a panic prevents dispatch, and cancel each derived attempt context when
  its operation or permit completion panics, preserving error and panic identity.

### Security

- Document execution and adapter trust boundaries and explicit caller-owned
  residual risks for the v2 API family.

## [1.1.0] - 2026-09-06

### Added

- Add `NewPolicyStrict` and `DoStrict` with explicit known, not-dispatched, and
  unknown outcomes, bounded terminal errors, and normalized cancellation.
- Add target-oriented HTTP, PostgreSQL, slog, and OpenTelemetry adapters with
  their own public type identities.
- Add `retrytelemetry.NewStrict` for typed-nil meter-provider validation while
  retaining the legacy instrumentation scope.

### Deprecated

- Prefer `adapters/http`, `adapters/postgres`, `adapters/slog`, and
  `adapters/otel` over the released flat adapter paths. The old paths remain
  supported through the documented compatibility interval.

### Changed

- Make the strict HTTP classifier reject invalid and duplicate statuses and
  bound retained `Retry-After` metadata without exposing cause text.
- Give strict PostgreSQL classification caller cancellation and deadline
  precedence over otherwise transient backend failures.

- Adopt the `go-library-tools` v1.4.0 schema-v2 cohesion contract and local
  `make cohesion` gate without changing the retry API or runtime behavior.
- Pin reusable CI to the v1.4.0 W14 workflow and enforce cohesion metadata in
  the repository's required CI contract.
- Correct the recorded `go-resilience` v1.0.0 archive checksum to the published
  module identity without changing the selected dependency version.

- Adopt the released `go-library-tools` v1.0.5 repository contract and remove
  the duplicated repository-local verification implementation while preserving
  retry-specific policy, evidence, and fixtures.

### Fixed

- Use a fixed execution budget for policy-validation fuzzing so a completed
  run cannot fail solely at the wall-clock budget boundary.

### Documentation

- Publish the module's family, capabilities, ownership, lifecycle, supported
  environments, package selection, and delivery status, and link the README to
  the immutable v1.4.0 ecosystem index and family guidance.

- Replace archived monorepo links and completed execution artifacts with a
  standalone, human-oriented documentation structure.

## [1.0.0] - 2026-08-25

### Changed

- Exclude intentional nested modules from root local-proxy archives so local,
  bootstrap, CI, and public module checksums describe the same source
  boundary.

- Track the pinned documentation-tool lockfile so clean CI checkouts install
  the exact validated cspell dependency.

- Reconcile standalone dependency checksums against deterministic current
  module archives so CI, local verification, and release consumers resolve
  identical content.

- Harden standalone documentation validation with deterministic spelling and
  link checks, package-specific documentation gates, and repository-local
  contributor guidance.

### Documentation

- Link the package README to package-owned documentation.

### Changed

- Publish the module from its standalone `github.com/faustbrian/go-retry` identity while preserving its documented API and behavior.
- Replace the obsolete owned-module pseudo-version pin with the monorepo's
  local `v0.0.0` source-proxy coordinate; release tooling continues to emit
  the exact `v1.0.0` dependency version.

### Fixed

- Parse `Retry-After` delta seconds directly in the signed duration domain so
  saturation remains explicit without a narrowing integer conversion.
- Upgrade `golang.org/x/text` to v0.41.0 so the dependency graph no longer
  contains GO-2026-5970.
- Zero-unit Fibonacci backoff now returns within a fixed computation bound even
  when callers supply the largest possible attempt number.
- The shared resilience dependency now uses an immutable published revision so
  clean consumers can resolve Retry with workspace resolution disabled.
- PostgreSQL retry classification now recognizes pgx-safe, closed-connection,
  timeout, truncated-response, and network failures as transient while
  preserving caller cancellation and deadlines as permanent.
- The module actionlint gate now validates the repository-owned CI workflow
  instead of requiring a forbidden package-local workflow.

### Added

- Opt-in consumption of the shared `resilience` work budget, with coordinated
  retry lineage, local-denial errors, and retry-plus-hedge amplification proof.
- Explicit bounded retry policies and generic value execution.
- Nine deterministic and jittered backoff strategy families.
- Typed terminal errors, bounded history, and delay hints.
- HTTP, pgx, queue, webhook, filesystem, object-storage, slog, and
  OpenTelemetry adapters.
- Coverage, fuzz, race, leak, mutation, API, documentation, and benchmark
  gates.

[Unreleased]: https://github.com/faustbrian/go-retry/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/faustbrian/go-retry/compare/v1.1.0...v2.0.0
[1.1.0]: https://github.com/faustbrian/go-retry/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/faustbrian/go-retry/releases/tag/v1.0.0
