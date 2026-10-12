# Contributing

## Before Editing

1. Read the README, [`AGENTS.md`](AGENTS.md) and the affected API contract.
2. Start with `go test ./...` or a focused package/test selector.
3. Inspect direct consumers when changing a public or composition contract.
4. Preserve unrelated work and generated/corpus provenance.
5. Use the released tool selected by [`.golib.yaml`](.golib.yaml); do not add
   repository-local verification scripts or workflow implementations.

An ordinary fix stays in this repository. It does not require sibling checkouts,
Tools edits, ecosystem catalog updates or agent-goal ledgers. Change manifests
only when their owned facts change, not merely because source changed.

## Changes

Keep commits focused and conventional. Update changelogs for material public
behavior, compatibility or adoption changes, not CI-only administration.
Public API changes require compatibility
evidence and documentation. Specification behavior requires a decision record,
fixture coverage, and interoperability evidence.

New direct dependencies and dependency updates must follow the
[dependency governance policy](AGENTS.md#dependencies-and-supply-chain). Package-local
update bots are forbidden; the root policy owns every module and action update.

Specification-backed changes must follow the
[specification governance contract](AGENTS.md#design), update
the affected stable decision entries, and complete the Specification Decisions
section of the pull request template. An unresolved interpretation or stale
source pin is release-blocking; peer behavior cannot silently select policy.

Mutation is optional developer-local or selected scheduled assurance, not a PR
prerequisite. Investigate viable survivors and add focused regressions for real
defects; moving a campaign off PRs does not waive a confirmed correctness or
security defect. Equivalent/invalid mutants retain reviewed narrow dispositions.

Do not add package-local workflows, permanent replacements, machine-specific
paths, bypass flags, broad mutation exclusions, or aggregate quality metrics
that hide a failing package.

## Verification

For ordinary source work, `go test ./...` runs deterministic tests, fuzz seeds
and saved corpus regressions. Use the qualified public Golib v2.4.0 verifier
declared in `.golib.yaml` for repository checks:

```bash
make inventory
make check
make ci
```

`check` and `ci` use bounded local checks, including one named race selection
for the owned concurrency/lifecycle assertions in `.golib.yaml`; they do not run
full mutation or repeated assurance campaigns. Documentation changes need relevant
structure, links and examples, not unrelated runtime work.
Retry dispatch, cancellation, budget and adapter changes retain their affected
composition, API and direct-consumer evidence. Retryable classification alone
never proves that an operation is safe to repeat.

For active exploration, select one fuzz target and a small budget; long campaigns
belong in scheduled assurance. Optional local mutation selects one package with
the qualified verifier. These examples are not all-change requirements:

```bash
GOMAXPROCS=2 go test -run '^$' -fuzz '^FuzzBackoffNeverProducesNegativeDelay$' -fuzztime=2s -parallel=1 -timeout=2m .
golib mutation --module . --package .
```

`make check-full` retains explicit full assurance, including all configured
full operations and original fuzz budgets. Relevant release qualification uses
`golib release dry-run --module .`. Select these for their material risk or
release boundary, not every commit. Report unavailable or failed checks honestly;
a fast local pass is not public-release or hosted-CI qualification.
CI/configuration-only changes do not require a module release.

## Selected Monthly Assurance

[`assurance.yml`](.github/workflows/assurance.yml) runs on main at 08:17 UTC on
the 8th day of each month. It selects one of the 5 declared root-module
package/target pairs using the shared UTC-week selector, not full campaigns or
a guaranteed complete monthly rotation. Duplicate names in different packages
remain separate risks. Explicit main-only dispatch accepts a declared identity
in `package::^FuzzName$` form, for example
`./adapters/http::^FuzzParseRetryAfterNeverReturnsNegativeDelay$`.
A bare name is accepted only when unique; inputs are validated as declared
identities and are not forwarded as shell commands.

The selected risks are nonnegative backoff, contradictory delay-bound rejection,
Retry-After parsing and bounded response metadata. Existing assertions compare
canonical/legacy parsing and retain accepted causes or typed refusal errors.
This does not establish operation replay safety, concurrent execution, admission
cleanup, timer cancellation or adapter composition; retain affected lifecycle,
race, public API and direct-consumer checks.

The immutable shared runner bounds active fuzzing to 30 seconds, two workers
and two CPUs, with a five-minute combined native session, 2 GiB memory and
1 MiB output limits inside a ten-minute job. It installs the declared public
CLI without a source build, uses private disposable caches and keeps failures,
timeouts and exhausted budgets visible. Ordinary deterministic tests still run
seeds/corpus; ordinary CI and its Required contract are unchanged. Full/manual
operations and their original budgets remain in `.golib.yaml`.

Treat findings as owner-attributed diagnostics: promptly repair viable defects
with focused regressions; retain reviewed narrow equivalent/invalid dispositions
rather than weakening tests. Review cadence against useful findings and actual
cost; do not interpret a selected pass as full or release qualification.

## Adding A Module

Follow [repository structure policy](AGENTS.md#repository-structure). New modules
require an explicit purpose, ownership boundary, dependency review, package
catalog entry, full quality gates, documentation, changelog, license, security
policy, compatibility plan, and release dry-run.

## Source Documentation

Install the checksum-pinned public verifier declared in `.golib.yaml`.
`make source-docs-check` validates objective Go comment, marker, link and
generated-producer structure; `make check`, `make check-full` and `make ci`
require it before their existing checks. Shared CI runs the same strict
contract and includes its result in Required. This is structural enforcement,
not a substitute for source-truth review or behavioral verification.
