# Makefile -- local pre-deploy test gate for SpamFilter.
#
# "Comprehensive testing automatically run before any deployment" is
# enforced here, not in cloud CI: `make deploy` depends on `make test`,
# and `make test` runs the FULL suite (Go + iOS). Make's prerequisite
# ordering means a failing test aborts the chain before any deploy step
# runs. The `hooks` target wires the same gate into `git push` via
# .githooks/pre-push.

SHELL := /usr/bin/env bash

.PHONY: help test-go test-ios test deploy hooks

## help: List available targets (default target).
help:
	@echo "SpamFilter -- available targets:"
	@echo "  make test-go   Run the Go suite (go test ./... -race -cover)"
	@echo "  make test-ios  Run the iOS suite (xcodegen + xcodebuild test) on an available simulator"
	@echo "  make test      Run the full suite: test-go AND test-ios (the pre-deploy gate)"
	@echo "  make deploy    Run 'test' first (hard prerequisite), then scripts/deploy.sh"
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

## deploy: 'test' is a hard prerequisite so a red suite aborts here,
## before scripts/deploy.sh (or any deploy step) ever runs.
deploy: test
	@if [ -x scripts/deploy.sh ]; then \
		./scripts/deploy.sh; \
	else \
		echo "scripts/deploy.sh not found or not executable -- skipping deploy step." >&2; \
	fi

## hooks: Install the pre-push gate and point git at .githooks.
hooks:
	chmod +x .githooks/pre-push
	git config core.hooksPath .githooks
	@echo "Installed: git config core.hooksPath -> .githooks"
