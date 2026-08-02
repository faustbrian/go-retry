# Goal: Retry Resilience Integration

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Extend the implemented `retry` package with the resilience behavior needed for
safe composition under Kubernetes-scale concurrency. This supplements, and
MUST NOT rewrite or weaken, the executed base goals.

## Required Additions

- Add shared retry budgets that bound additional attempts across concurrent
  logical operations by a stable, bounded resource identity.
- Support explicit result/error abort, retry, and ignore classification without
  making protocol policy part of the core.
- Ensure maximum attempts, total elapsed time, total sleep, per-attempt timeout,
  caller deadline, and shared budget all compose with the earliest bound
  winning.
- Preserve server-directed delay such as `Retry-After` only within caller and
  policy limits; reject malformed, excessive, past, overflowed, or untrusted
  values deterministically.
- Emit attempt, scheduled delay, budget denial, exhaustion, abort, cancellation,
  and completion observations with bounded metadata.
- Keep timers, random sources, histories, and resource registries bounded and
  injectable.

## Kubernetes And Amplification

Budgets are process-local unless a separately reviewed distributed adapter is
selected. Documentation MUST calculate fleet amplification from attempts,
request fan-out, pagination, replicas, and HPA maximum replicas. Scale-up and
rolling updates reset local budgets and can create synchronized retries, so
defaults MUST use jitter and conservative cold-start behavior.

SIGTERM MUST stop scheduling new attempts, propagate cancellation, release
timers, and report ambiguous in-flight side effects honestly. Retry exhaustion
or downstream failure MUST NOT fail process liveness.

## Composition

Document and test retry around versus inside breaker, rate limit, bulkhead,
adaptive concurrency, adaptive throttle, per-attempt timeout, cache, and hedge.
Local admission rejections MUST not be retried by default. Retry and hedge MUST
share one hard work-amplification budget. Every operation adapter MUST state
the idempotency and replay contract.

## Deliverables

- API and migration documentation for shared budgets and classification.
- Kubernetes sizing, rollout, drain, retry-storm, and HPA guidance.
- Equivalent-behavior comparisons with current Failsafe-Go and maintained retry
  packages.
- Changelog and compatibility treatment for every public addition.

## Acceptance Criteria

- No logical operation or resource exceeds configured retry bounds.
- Budget denial cannot be misclassified as downstream failure.
- Policy ordering and timeout scope are executable and documented.
- Existing callers retain behavior unless they opt into a new policy.
- Meaningful exact 100% statement coverage and exactly 100% viable mutation
  kills remain blocking requirements.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
