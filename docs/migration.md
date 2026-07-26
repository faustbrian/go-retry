# Migration

1. Inventory every existing loop and the exact failures it retries.
2. Prove repeat safety independently of error transience.
3. Convert implicit retry rules into an explicit classifier.
4. Choose a strategy and calculate attempt, elapsed, per-attempt, delay, and
   sleep bounds.
5. Inject clock, sleeper, random source, and observer dependencies.
6. Compare old and new attempt counts and terminal causes in shadow metrics.
7. Remove nested library retries or include them in the total budget.

When migrating from cenkalti/backoff or avast/retry-go, note that `retry`
rejects zero attempts and never supplies an implicit classifier or policy.
