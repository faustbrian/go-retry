# Verification

`make check` is the blocking local gate. It verifies formatting, module
tidiness, vet, architecture boundaries, unit tests, race safety, exact 100%
production statement coverage, deterministic fuzz smoke, mutation checks,
leak detection, comparative benchmarks, documentation, public API, workflow
syntax, lint, Staticcheck, and vulnerability analysis.

The deterministic strategy vectors include overflow and saturation. Jitter
tests use seeded sources and fixed statistical tolerances. Cancellation tests
cover before-attempt, in-attempt, and in-sleep precedence. HTTP tests cover
seconds, dates, clock skew, malformed input, and oversized values.

Hosted CI must run the same commands on the exact commit before release.
