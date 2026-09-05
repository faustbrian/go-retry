# Operations

Use full or decorrelated jitter during broad outages. Set maximum delay below
the caller deadline and reserve time for cleanup. Keep history small; it retains
error causes for diagnosis. Treat exhaustion spikes as dependency or capacity
signals rather than increasing attempts automatically.

During an incident:

1. verify retry volume and exhaustion reason;
2. confirm total attempts including nested SDK or proxy retries;
3. reduce amplification through configuration or traffic controls;
4. preserve caller cancellation;
5. change retry classification only with protocol evidence.

When `DoStrict` returns `ErrOutcomeUnknown`, stop automatic retries and
reconcile the operation through its idempotency key, transaction identity,
provider lookup, or another authoritative record. Cancellation classification
does not make the operation safe to replay. Preserve the returned retry reason
and machine category in telemetry without logging the original cause text.

The package creates no worker or registry and requires no shutdown lifecycle.
