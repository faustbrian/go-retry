# Changelog

All notable changes use [Keep a Changelog](https://keepachangelog.com/) style.

## [Unreleased]

### Documentation

- Link the package README to the repository-wide Golib documentation portal.

### Changed

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
