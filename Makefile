GO ?= go
CMD_PATH := ./cmd/symphony
BIN_DIR := bin
BIN := $(BIN_DIR)/symphony

.PHONY: all build clean cli-help cli-run-help fmt harness-check harness-review-gate harness-verify help test test-fake-e2e tidy verify

all: build

help:
	@printf '%s\n' \
		'Targets:' \
		'  make fmt                 Run gofmt on Go sources' \
		'  make tidy                Run go mod tidy' \
		'  make test                Run go test ./...' \
		'  make test-fake-e2e       Run deterministic fake Linear + fake Codex E2E profile' \
		'  make build               Build bin/symphony' \
		'  make cli-help            Smoke test root CLI help' \
		'  make cli-run-help        Smoke test run command help' \
		'  make harness-check       Run harness consistency checks' \
		'  make verify              Run fmt, tidy, tests, build, CLI help, and harness checks' \
		'  make clean               Remove build outputs'

fmt:
	$(GO)fmt -w cmd internal

tidy:
	$(GO) mod tidy

test:
	$(GO) test ./...

test-fake-e2e:
	$(GO) test ./internal/orchestrator -run TestFakeE2EProfile -count=1

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN) $(CMD_PATH)

cli-help:
	$(GO) run $(CMD_PATH) --help

cli-run-help:
	$(GO) run $(CMD_PATH) run --help

verify: fmt tidy test build cli-help cli-run-help harness-check

clean:
	rm -rf $(BIN_DIR)

harness-check:
	bash scripts/harness/check.sh

harness-verify: harness-check

harness-review-gate:
	@if [ -z "$(PLAN)" ]; then echo "usage: make harness-review-gate PLAN=path/to/plan.md" >&2; exit 2; fi
	bash scripts/harness/review_gate.sh --plan "$(PLAN)"
