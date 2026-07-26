#!/usr/bin/env bash
set -euo pipefail

current=$(mktemp)
trap 'rm -f "$current"' EXIT
while IFS= read -r package; do
	printf 'PACKAGE %s\n' "$package" >> "$current"
	go doc -all "$package" >> "$current"
done < <(go list ./...)
perl -0pi -e 's/\n+\z/\n/' "$current"
if [[ "${1:-}" == "--update" ]]; then
	mkdir -p api
	cp "$current" api/baseline.txt
	exit 0
fi
if [[ ! -f api/baseline.txt ]]; then
	echo 'api/baseline.txt is missing; run make api-update' >&2
	exit 1
fi
diff -u api/baseline.txt "$current"
