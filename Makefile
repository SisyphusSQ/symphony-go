GO ?= go
CMD_PATH := ./cmd/symphony
BIN_DIR := bin
BIN := $(BIN_DIR)/symphony
VERSION ?= v1.0.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
RELEASE_DIR := build/release/$(VERSION)
PACKAGES_DIR := $(RELEASE_DIR)/packages
EMBED_DASHBOARD_DIR := internal/server/embedded_dashboard/dist
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

.PHONY: all build clean cli-help cli-run-help embed-dashboard fmt harness-check harness-review-gate harness-verify help release release-checksums release-clean release-linux-amd64 release-macos-arm64 release-notes release-windows-amd64 test test-fake-e2e test-real-integration tidy verify version web-build

all: build

help:
	@printf '%s\n' \
		'Targets:' \
		'  make fmt                 Run gofmt on Go sources' \
		'  make tidy                Run go mod tidy' \
		'  make test                Run go test ./...' \
		'  make test-fake-e2e       Run deterministic fake Linear + fake Codex E2E profile' \
		'  make test-real-integration Run opt-in real Linear/Codex dogfood profile' \
		'  make build               Build bin/symphony' \
		'  make version             Show build metadata from bin/symphony' \
		'  make cli-help            Smoke test root CLI help' \
		'  make cli-run-help        Smoke test run command help' \
		'  make web-build           Build operator Web GUI into web/dist' \
		'  make embed-dashboard     Sync web/dist into Go embedded assets' \
		'  make release VERSION=vX.Y.Z Build linux/windows/macos release packages' \
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

test-real-integration:
	$(GO) test ./internal/orchestrator -run TestRealIntegrationProfile -count=1 -v

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN) $(CMD_PATH)

version: build
	$(BIN) version

cli-help:
	$(GO) run $(CMD_PATH) --help

cli-run-help:
	$(GO) run $(CMD_PATH) run --help

verify: fmt tidy test build cli-help cli-run-help harness-check

clean:
	rm -rf $(BIN_DIR)

web-build:
	cd web && npm run build

embed-dashboard: web-build
	test -f web/dist/index.html
	mkdir -p $(EMBED_DASHBOARD_DIR)
	find $(EMBED_DASHBOARD_DIR) -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	cp -R web/dist/. $(EMBED_DASHBOARD_DIR)/

release-clean:
	rm -rf $(RELEASE_DIR)

release-notes:
	mkdir -p $(RELEASE_DIR)
	{ \
		printf '%s\n' '# symphony-go $(VERSION)'; \
		printf '%s\n\n' 'Go implementation of the Symphony single-instance issue orchestration runtime.'; \
		printf '%s\n' '## Artifacts'; \
		printf '%s\n' '- Linux amd64: `symphony-go_$(VERSION)_linux_amd64.tar.gz`'; \
		printf '%s\n' '- Windows amd64: `symphony-go_$(VERSION)_windows_amd64.zip`'; \
		printf '%s\n\n' '- macOS arm64: `symphony-go_$(VERSION)_macos_arm64.tar.gz`'; \
		printf '%s\n' 'Each package contains one backend binary with the operator Web GUI embedded, plus `README.md`, `ChangeLog.md`, `WORKFLOW.md`, and `START.md`.'; \
		printf '%s\n\n' 'See `ChangeLog.md` for the full release history.'; \
		printf '%s\n' '## Quick Start'; \
		printf '%s\n' '```bash'; \
		printf '%s\n' './symphony validate --workflow WORKFLOW.md'; \
		printf '%s\n' './symphony run --workflow WORKFLOW.md --port 4002 --instance local'; \
		printf '%s\n' '```'; \
		printf '%s\n\n' 'On Windows, use `symphony.exe` for the same commands.'; \
	} > $(RELEASE_DIR)/RELEASE_NOTES.md
	{ \
		printf '%s\n' '# Start symphony-go'; \
		printf '%s\n\n' 'Prepare the environment variables referenced by `WORKFLOW.md` before running Symphony.'; \
		printf '%s\n' '```bash'; \
		printf '%s\n' './symphony validate --workflow WORKFLOW.md'; \
		printf '%s\n' './symphony run --workflow WORKFLOW.md --port 4002 --instance local'; \
		printf '%s\n' '```'; \
		printf '%s\n\n' 'On Windows, use `symphony.exe` for the same commands.'; \
		printf '%s\n' 'The operator Web GUI is embedded in the binary and is served from the same local operator HTTP server.'; \
	} > $(RELEASE_DIR)/START.md

release-linux-amd64: embed-dashboard release-notes
	mkdir -p $(PACKAGES_DIR)
	rm -rf $(RELEASE_DIR)/symphony-go_$(VERSION)_linux_amd64
	mkdir -p $(RELEASE_DIR)/symphony-go_$(VERSION)_linux_amd64
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(RELEASE_DIR)/symphony-go_$(VERSION)_linux_amd64/symphony $(CMD_PATH)
	cp README.md ChangeLog.md WORKFLOW.md $(RELEASE_DIR)/START.md $(RELEASE_DIR)/symphony-go_$(VERSION)_linux_amd64/
	tar -C $(RELEASE_DIR) -czf $(PACKAGES_DIR)/symphony-go_$(VERSION)_linux_amd64.tar.gz symphony-go_$(VERSION)_linux_amd64

release-windows-amd64: embed-dashboard release-notes
	mkdir -p $(PACKAGES_DIR)
	rm -rf $(RELEASE_DIR)/symphony-go_$(VERSION)_windows_amd64
	mkdir -p $(RELEASE_DIR)/symphony-go_$(VERSION)_windows_amd64
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(RELEASE_DIR)/symphony-go_$(VERSION)_windows_amd64/symphony.exe $(CMD_PATH)
	cp README.md ChangeLog.md WORKFLOW.md $(RELEASE_DIR)/START.md $(RELEASE_DIR)/symphony-go_$(VERSION)_windows_amd64/
	cd $(RELEASE_DIR) && zip -qr packages/symphony-go_$(VERSION)_windows_amd64.zip symphony-go_$(VERSION)_windows_amd64

release-macos-arm64: embed-dashboard release-notes
	mkdir -p $(PACKAGES_DIR)
	rm -rf $(RELEASE_DIR)/symphony-go_$(VERSION)_macos_arm64
	mkdir -p $(RELEASE_DIR)/symphony-go_$(VERSION)_macos_arm64
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(RELEASE_DIR)/symphony-go_$(VERSION)_macos_arm64/symphony $(CMD_PATH)
	cp README.md ChangeLog.md WORKFLOW.md $(RELEASE_DIR)/START.md $(RELEASE_DIR)/symphony-go_$(VERSION)_macos_arm64/
	tar -C $(RELEASE_DIR) -czf $(PACKAGES_DIR)/symphony-go_$(VERSION)_macos_arm64.tar.gz symphony-go_$(VERSION)_macos_arm64

release-checksums:
	cd $(PACKAGES_DIR) && shasum -a 256 symphony-go_$(VERSION)_* > SHA256SUMS.txt

release: release-clean release-linux-amd64 release-windows-amd64 release-macos-arm64 release-checksums

harness-check:
	bash scripts/harness/check.sh

harness-verify: harness-check

harness-review-gate:
	@if [ -z "$(PLAN)" ]; then echo "usage: make harness-review-gate PLAN=path/to/plan.md" >&2; exit 2; fi
	bash scripts/harness/review_gate.sh --plan "$(PLAN)"
