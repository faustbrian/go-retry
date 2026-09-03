# Changelog

All notable changes use [Keep a Changelog](https://keepachangelog.com/) style.

## [Unreleased]

### Changed

- Adopt the `go-library-tools` v1.3.0 schema-v2 cohesion contract and local
  `make cohesion` gate without changing the retry API or runtime behavior.
- Pin reusable CI to the v1.3.0 workflow and enforce cohesion metadata in the
  repository's required CI contract.

- Adopt the released `go-library-tools` v1.0.5 repository contract and remove
  the duplicated repository-local verification implementation while preserving
  retry-specific policy, evidence, and fixtures.

### Documentation

- Publish the module's family, capabilities, ownership, lifecycle, supported
  environments, package selection, and delivery status, and link the README to
  the immutable v1.3.0 ecosystem index and family guidance.

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

[Unreleased]: https://github.com/faustbrian/go-retry/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/faustbrian/go-retry/releases/tag/v1.0.0
