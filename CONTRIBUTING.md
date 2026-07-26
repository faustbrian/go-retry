# Contributing

Use Go 1.26.5 or newer within the Go 1 compatibility promise. Keep retry
classification separate from repeat safety and add a failing test before each
behavior change.

Run `make check` before submitting changes. Run `make check-all` for the full
local release gate. Public API changes require `make api-update` plus an entry
in `CHANGELOG.md`.
