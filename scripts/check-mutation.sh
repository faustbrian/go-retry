#!/usr/bin/env bash
set -euo pipefail

workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT
baseline="$workspace/baseline"
mkdir -p "$baseline"
tar --exclude=.git --exclude=.tools --exclude=coverage.out --exclude=dist -cf - . | tar -xf - -C "$baseline"

run_mutant() {
	local name=$1 file=$2 from=$3 to=$4
	local mutant="$workspace/$name"
	mkdir -p "$mutant"
	tar -cf - -C "$baseline" . | tar -xf - -C "$mutant"
	FROM="$from" TO="$to" perl -0pi -e '
$changed = s/\Q$ENV{FROM}\E/$ENV{TO}/;
END { die "mutation source not found: $ENV{FROM}\n" unless $changed }
' "$mutant/$file"
	if (cd "$mutant" && go test -timeout=15s ./... >mutation.log 2>&1); then
		echo "survived mutation: $name" >&2
		cat "$mutant/mutation.log" >&2
		exit 1
	fi
	printf 'killed mutation: %s\n' "$name"
	rm -rf "$mutant"
}

run_mutant attempt_bound execution.go 'attempt == policy.config.MaxAttempts' 'attempt+1 == policy.config.MaxAttempts'
run_mutant cancellation execution.go 'if err := ctx.Err(); err != nil {' 'if err := ctx.Err(); err == nil {'
run_mutant post_attempt_cancellation execution.go $'result.Attempts = attempt\n\n\t\tif err := ctx.Err(); err != nil {' $'result.Attempts = attempt\n\n\t\tif err := ctx.Err(); err == nil {'
run_mutant permanent_classification execution.go 'classification == ClassificationPermanent' 'classification == ClassificationRetryable'
run_mutant elapsed_bound execution.go 'delay > policy.config.MaxElapsed-currentElapsed' 'delay < policy.config.MaxElapsed-currentElapsed'
run_mutant sleep_bound execution.go 'delay > policy.config.MaxSleep-totalSleep' 'delay < policy.config.MaxSleep-totalSleep'
run_mutant delay_hint execution.go 'hinted > delay' 'hinted < delay'
run_mutant history_bound execution.go 'uint(len(result.History)) == limit' 'uint(len(result.History)) < limit'
run_mutant observer_panic execution.go 'defer func() { _ = recover() }()' 'defer func() {}()'
run_mutant max_attempts policy.go 'config.MaxAttempts == 0' 'config.MaxAttempts != 0'
run_mutant delay_order policy.go 'config.MinDelay > config.MaxDelay' 'config.MinDelay < config.MaxDelay'
run_mutant maximum_delay policy.go 'delay > policy.config.MaxDelay' 'delay < policy.config.MaxDelay'
run_mutant overflow backoff.go 'signedMultiplier > maxDuration/value' 'signedMultiplier < maxDuration/value'
run_mutant retry_after retryhttp/http.go 'delay < 0' 'delay > 0'
run_mutant http_status retryhttp/http.go 'http.StatusTooManyRequests' 'http.StatusCreated'
run_mutant sqlstate retrypgx/classifier.go '"40001"' '"40002"'

echo 'mutation score: 16/16 killed (100.0%)'
