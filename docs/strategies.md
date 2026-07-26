# Strategy selection

All attempts are one-based: attempt `1` is the delay before the first retry.
Arithmetic saturates at the largest `time.Duration` and policy delay bounds
are applied after strategy calculation.

| Strategy | Use |
| --- | --- |
| Constant | Polling with a known fixed cadence. |
| Linear | Gentle load growth with predictable spacing. |
| Polynomial | Workloads needing stronger-than-linear growth. |
| Fibonacci | Moderate growth without exponential jumps. |
| Exponential | Default for transient distributed-system failures. |
| Full jitter | Maximum spreading during synchronized failure storms. |
| Equal jitter | Retain a minimum pause while spreading callers. |
| Exponential jitter | Center randomness around exponential delay. |
| Decorrelated jitter | Avoid correlated sequences across repeated failures. |

Jitter strategies require an injected concurrency-safe `Random`. A nil random
source selects the lower bound deterministically; production policies should
inject a suitably seeded source. `NewRandom` is deterministic and not
cryptographic.
