#!/usr/bin/env bash
set -euo pipefail

required=(
	README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md CODE_OF_CONDUCT.md
	LICENSE NOTICE THIRD_PARTY_LICENSES.md docs/README.md docs/strategies.md
	docs/idempotency.md docs/budgets.md docs/adapters.md docs/composition.md
	docs/observability.md docs/migration.md docs/operations.md
	docs/compatibility.md docs/faq.md docs/verification.md
)
for file in "${required[@]}"; do
	if [[ ! -s "$file" ]]; then
		echo "required documentation is missing or empty: $file" >&2
		exit 1
	fi
done
python3 - <<'PY'
from pathlib import Path
import re
for document in Path('.').rglob('*.md'):
    content = document.read_text(encoding='utf-8')
    prose, fenced = [], False
    for line in content.splitlines():
        if line.lstrip().startswith(('```', '~~~')):
            fenced = not fenced
            continue
        if not fenced:
            prose.append(line)
    for target in re.findall(r'\[[^]]*\]\(([^)]+)\)', '\n'.join(prose)):
        if target.startswith(('http://', 'https://', 'mailto:', '#')):
            continue
        relative = target.split('#', 1)[0]
        if relative and not (document.parent / relative).resolve().exists():
            raise SystemExit(f'broken relative link in {document}: {target}')
print('documentation links resolve')
PY
go test ./... -run '^Example'
