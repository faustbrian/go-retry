GO ?= go
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.6.0
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@v0.7.0
ACTIONLINT ?= go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
NILAWAY ?= go run go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260710181136-2378218750e4
FUZZ_TIME ?= 2s
BENCH_TIME ?= 100ms

.PHONY: actionlint api-compat api-update architecture benchmark check check-all \
	coverage docs format format-check fuzz leak lint mutation nilaway race \
	staticcheck test tidy-check vet vuln

format:
	gofmt -w .

format-check:
	test -z "$$(gofmt -l .)"

tidy-check:
	$(GO) mod tidy -diff

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	./scripts/check-coverage.sh

vet:
	$(GO) vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout=5m ./...

staticcheck:
	$(STATICCHECK) ./...

actionlint:
	$(ACTIONLINT) .github/workflows/*.yml

architecture:
	./scripts/check-architecture.sh

fuzz:
	./scripts/check-fuzz.sh "$(FUZZ_TIME)"

mutation:
	./scripts/check-mutation.sh

leak:
	$(GO) test . -run '^TestNoPackageBackgroundWorkers$$' -count=10

benchmark:
	$(GO) test . -run '^$$' -bench Benchmark -benchmem \
		-benchtime="$(BENCH_TIME)"

docs:
	./scripts/check-docs.sh

api-compat:
	./scripts/check-api-compat.sh

api-update:
	./scripts/check-api-compat.sh --update

vuln:
	$(GOVULNCHECK) ./...

nilaway:
	$(NILAWAY) -include-pkgs='github.com/faustbrian/golib/pkg/retry/...' ./...

check: tidy-check format-check vet architecture test race coverage fuzz \
	mutation leak benchmark docs api-compat actionlint lint staticcheck vuln

check-all: check nilaway
