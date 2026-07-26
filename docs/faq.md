# FAQ

## Why is `MaxAttempts` mandatory?

It makes infinite retry unrepresentable. Attempts include the first call.

## Does `Retryable` mean safe to repeat?

No. It only supplies classification evidence to a classifier.

## Why inject a clock and sleeper?

Budgets and cancellation can then be tested deterministically without real
waiting. Production may use `SystemClock` and `SystemSleeper`.

## Are panics retried?

No. Operation panics propagate immediately. Classifier and observer panics are
contained and stop or isolate execution respectively.

## Can Retry-After exceed policy limits?

No. Delay hints are clamped by maximum delay and checked against elapsed and
sleep budgets.
