#!/usr/bin/env bash
set -euo pipefail

duration=${1:-2s}
targets=(
	'.|FuzzBackoffNeverProducesNegativeDelay'
	'.|FuzzPolicyValidationDoesNotAcceptContradictoryDelayBounds'
	'./retryhttp|FuzzParseRetryAfterNeverReturnsNegativeDelay'
)
for entry in "${targets[@]}"; do
	package=${entry%%|*}
	target=${entry#*|}
	go test "$package" -run '^$' -fuzz "^${target}$" -fuzztime="$duration" -parallel=4
done
