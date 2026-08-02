# Goal: Harden Retry Resilience Integration

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Required Proof

- Race shared budget admission/refund, attempt completion, cancellation,
  deadline, sleep, shutdown, and policy reset.
- Test ambiguous outcomes, non-idempotent adapters, malformed `Retry-After`,
  timer cleanup, classifier panic/reentrancy, and uncooperative attempts.
- Simulate thousands of synchronized callers, pod scale-up, mixed revisions,
  downstream outage/recovery, and SIGTERM to prove bounded amplification.
- Test every supported composition so local rejection is not mistaken for a
  retryable dependency failure and retry plus hedge cannot multiply silently.
- Prove bounded attempt history, key cardinality, observations, timers, and
  goroutines.

Meaningful exact 100% statement coverage, exactly 100% viable mutation kills,
race, fuzz, stress, leak, fault, benchmark, API compatibility, docs, security,
and supply-chain gates MUST pass. Final review MUST find no unbounded retry,
budget leak, false cancellation claim, synchronized default, or undocumented
composition.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
