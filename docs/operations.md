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

The package creates no worker or registry and requires no shutdown lifecycle.
