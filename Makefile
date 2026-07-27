# Makefile -- local pre-deploy test gate for SpamFilter.
#
# "Comprehensive testing automatically run before any deployment" is
# enforced here, not in cloud CI: `make deploy` depends on `make test`,
# and `make test` runs the FULL suite (Go + iOS). Make's prerequisite
# ordering means a failing test aborts the chain before any deploy step
# runs. The `hooks` target wires the same gate into `git push` via
# .githooks/pre-push.

SHELL := /usr/bin/env bash

.PHONY: help test-go test-ios test build-linux-arm64 deploy deploy-server hooks

# Deploy target host (pm-prod-spamfilter). Override on the command line.
DEPLOY_HOST ?= 10.30.1.244
# AL2023 disables direct root SSH ("Please login as the user ec2-user"), and
# that refusal message breaks scp. Connect as ec2-user and sudo on the far side.
DEPLOY_USER ?= ec2-user
# pm-prod-spamfilter is a t4g.large -- Graviton/arm64, NOT x86_64. Building
# amd64 here yields "exec format error" on the box.
GOOS_LINUX  := linux
GOARCH_ARM  := arm64

## help: List available targets (default target).
help:
	@echo "SpamFilter -- available targets:"
	@echo "  make test-go   Run the Go suite (go test ./... -race -cover)"
	@echo "  make test-ios  Run the iOS suite (xcodegen + xcodebuild test) on an available simulator"
	@echo "  make test      Run the full suite: test-go AND test-ios (the pre-deploy gate)"
	@echo "  make build-linux-arm64  Cross-compile server+recompute for the arm64 deploy host"
	@echo "  make deploy-server      Backend-only release: gated on test-go only"
	@echo "  make deploy    Full release: gated on 'test' (Go AND iOS), then scripts/deploy.sh"
	@echo "  make hooks     Install .githooks/pre-push and set core.hooksPath"

## test-go: Go unit/integration suite with race detector and coverage.
test-go:
	go test ./... -race -cover

## test-ios: iOS suite on a dynamically-resolved available simulator.
## Never hardcode a device name/UDID -- simulators available on this
## machine change over time (Xcode/OS updates), so resolve one at runtime.
## Match by UDID (not just name): xcodebuild's -destination defaults an
## unspecified OS to "latest", and a device name (e.g. "iPhone 16 Pro") may
## only exist under an older runtime on this machine, which would then fail
## to match. Selecting by id= sidesteps that ambiguity entirely.
## The env-gated IntegrationTests in SpamFilterKitTests self-skip (XCTSkip)
## when SPAMFILTER_INTEGRATION_BASE_URL is unset, so they don't fail this gate.
test-ios:
	@sim_line=$$(xcrun simctl list devices available 2>/dev/null | grep -E '^\s+iPhone' | head -n 1); \
	sim_id=$$(echo "$$sim_line" | grep -oE '[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}'); \
	sim_name=$$(echo "$$sim_line" | sed -E 's/^[[:space:]]*([^(]+) \(.*/\1/' | sed -E 's/[[:space:]]+$$//'); \
	if [ -z "$$sim_id" ]; then \
		echo "ERROR: no available iOS Simulator found (xcrun simctl list devices available)." >&2; \
		echo "Install/create an iPhone simulator via Xcode and retry." >&2; \
		exit 1; \
	fi; \
	echo "Using simulator: $$sim_name ($$sim_id)"; \
	cd ios && xcodegen generate && xcodebuild \
		-project SpamFilter.xcodeproj \
		-scheme SpamFilter \
		-sdk iphonesimulator \
		-destination "platform=iOS Simulator,id=$$sim_id" \
		test CODE_SIGNING_ALLOWED=NO

## test: The comprehensive pre-deploy gate -- both suites must pass.
test: test-go test-ios
	@echo "All suites passed (Go + iOS)."

## build-linux-arm64: Cross-compile the server and recompute binaries for the
## deploy host. Static (CGO_ENABLED=0) so nothing needs installing on the box,
## and migrations are go:embed'ed so the binary is fully self-contained.
build-linux-arm64:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=$(GOOS_LINUX) GOARCH=$(GOARCH_ARM) go build -trimpath -o bin/hushield-server ./cmd/server
	CGO_ENABLED=0 GOOS=$(GOOS_LINUX) GOARCH=$(GOARCH_ARM) go build -trimpath -o bin/hushield-recompute ./cmd/recompute
	@echo "--- verifying architecture (must say ARM aarch64) ---"
	@file bin/hushield-server bin/hushield-recompute
	@file bin/hushield-server | grep -q 'ARM aarch64' \
		|| { echo "ERROR: bin/hushield-server is not ARM aarch64 -- it will not run on $(DEPLOY_HOST)." >&2; exit 1; }

## deploy-server: Backend-only release. Gated on the Go suite ALONE -- a broken
## iOS simulator must never block a server hotfix, which is why this exists
## separately from 'deploy'.
deploy-server: test-go build-linux-arm64
	DEPLOY_HOST=$(DEPLOY_HOST) DEPLOY_USER=$(DEPLOY_USER) ./scripts/deploy.sh

## deploy: Full release. 'test' (Go AND iOS) is a hard prerequisite so a red
## suite aborts here before any deploy step runs.
deploy: test build-linux-arm64
	DEPLOY_HOST=$(DEPLOY_HOST) DEPLOY_USER=$(DEPLOY_USER) ./scripts/deploy.sh

## hooks: Install the pre-push gate and point git at .githooks.
hooks:
	chmod +x .githooks/pre-push
	git config core.hooksPath .githooks
	@echo "Installed: git config core.hooksPath -> .githooks"
