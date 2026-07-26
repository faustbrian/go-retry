#!/usr/bin/env bash
set -euo pipefail

production=$(mktemp)
trap 'rm -f "$production"' EXIT
rg --files -g '*.go' -g '!**/*_test.go' >"$production"
if xargs rg -n '"unsafe"|//go:linkname|import[[:space:]]+"C"|func[[:space:]]+init[[:space:]]*\(' <"$production"; then
	echo 'forbidden production runtime mechanism found' >&2
	exit 1
fi
dependencies=$(go list -deps -f '{{.ImportPath}}' .)
if printf '%s\n' "$dependencies" | grep -E '^github\.com/faustbrian/go-(circuit-breaker|rate-limit|queue|scheduler|idempotency|log|telemetry)(/|$)'; then
	echo 'an owning sibling package entered the root dependency graph' >&2
	exit 1
fi
CGO_ENABLED=0 go test ./...
echo 'architecture constraints pass'
