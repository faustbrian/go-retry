# Idempotency and ownership

Classification answers whether a failure appears transient. It does not answer
whether repeating an operation is safe. Before enabling retries, identify:

- which effects may have completed before the error was observed;
- whether an idempotency key, transaction, conditional write, or deduplication
  record protects those effects;
- whether response loss can cause an already-completed operation to repeat;
- who owns cleanup after partial success.

Reads are not automatically safe if they trigger locks, billing, audit writes,
or external calls. Writes are not automatically unsafe if their protocol has a
verified idempotency contract. That decision stays at the call site.
