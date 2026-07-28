# Changelog

All notable changes use [Keep a Changelog](https://keepachangelog.com/) style.

## [Unreleased]

### Fixed

- PostgreSQL retry classification now recognizes pgx-safe, closed-connection,
  timeout, truncated-response, and network failures as transient while
  preserving caller cancellation and deadlines as permanent.

### Added

- Explicit bounded retry policies and generic value execution.
- Nine deterministic and jittered backoff strategy families.
- Typed terminal errors, bounded history, and delay hints.
- HTTP, pgx, queue, webhook, filesystem, object-storage, slog, and
  OpenTelemetry adapters.
- Coverage, fuzz, race, leak, mutation, API, documentation, and benchmark
  gates.
