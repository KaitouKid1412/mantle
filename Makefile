# mantle build targets. Scoped targets (test-NN, fmt-NN) keep parallel plan
# sessions out of each other's packages; see CLAUDE.md.

GO ?= go

# Packages per plan (primary paths, see docs/plans/00-overview.md).
PKGS_01 = ./pkg/ext/... ./pkg/theme/... ./cmd/mantle-ui/... ./internal/app/... \
          ./internal/keymap/... ./internal/config/... ./internal/archtest/... \
          $(shell $(GO) list ./pkg/ui/... ./internal/testkit/... 2>/dev/null | grep -v -e /editor -e /diffview -e /enginefake)
PKGS_02 = ./pkg/proto/... ./internal/engine/... ./internal/testkit/enginefake/... \
          ./cmd/fakeclaude/... ./cmd/fakeapi/...
PKGS_03 = ./pkg/render/... ./pkg/ui/diffview/... ./features/transcript/...
PKGS_04 = ./pkg/ui/editor/... ./features/input/...
PKGS_05 = ./features/turn/...
PKGS_06 = ./internal/sessions/... ./features/sessions/...
PKGS_07 = ./internal/term/... ./features/chrome/...
PKGS_08 = ./features/settings/...
PKGS_09 = ./features/ecosystem/... ./internal/claudecli/...
PKGS_10 = ./cmd/mantle/... ./internal/launcher/... ./internal/install/... \
          ./features/launch/...
PKGS_11 = ./internal/cli/... ./features/cli/... ./scripts/drift/...
PKGS_12 = ./features/fullscreen/... ./test/...

.PHONY: build test lint vet archtest parity parity-side-by-side perf e2e-real drift tags sessions install

build:
	$(GO) build -o bin/ ./cmd/...

test:
	$(GO) test ./...

# make test-04: test only plan 04's packages.
test-%:
	$(GO) test $(PKGS_$*)

# make fmt-04: format only plan 04's packages (never gofmt -w the whole repo).
fmt-%:
	$(GO) fmt $(PKGS_$*)

vet:
	$(GO) vet ./...

lint: vet
	@out=$$(gofmt -l cmd features internal pkg test); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

archtest:
	$(GO) test ./internal/archtest/...

# Parity status (plan 12): every docs/PARITY.md row with its evidence (Parity tags on
# registrations, side-by-side scenarios) and plan 12's triage requests, written to
# docs/parity-status.md.
parity:
	$(GO) run ./test/parity/cmd/status

# Side-by-side parity harness (plan 12): runs test/parity/scenarios against the
# interactive targets with fakeapi (offline, free) and writes test/parity/out/.
# PARITY_ARGS="-run plain -targets claude" narrows the run.
parity-side-by-side:
	$(GO) run ./test/parity/cmd/sidebyside $(PARITY_ARGS)

# Performance suite (plan 12): mantle against claude on fakeapi (offline, free) for
# startup, idle CPU, streaming, large output, wide characters and a 10k-item resume.
# Writes test/parity/out/perf.md. MANTLE_PERF_N sets the startup runs (default 5).
perf:
	MANTLE_PERF=1 $(GO) test ./test/e2e/perf -v -count=1 -timeout 30m

# Opt-in real end-to-end check (plan 12): mantle with the real engine and API, an
# isolated config and the cheapest model; costs a few cents and prints /cost.
# MANTLE_E2E_REAL=1 ANTHROPIC_API_KEY=... make e2e-real
e2e-real:
	@if [ -z "$$MANTLE_E2E_REAL" ]; then echo "e2e-real costs money: set MANTLE_E2E_REAL=1 (and ANTHROPIC_API_KEY)"; exit 1; fi
	$(GO) test ./test/e2e/real -v -count=1 -timeout 20m

# Compare the installed claude's flags, subcommands, keybindings and settings keys with
# mantle's tables; writes docs/parity-drift.{md,json}. Exit 1 on unclassified items.
# DRIFT_ARGS="-offline" uses the cached settings schema; "-accept" moves the baseline.
drift:
	$(GO) run ./scripts/drift $(DRIFT_ARGS)

# Contract tags that unblock Part B of the plans.
tags:
	@git tag -l 'contracts-v*' 'proto-v*'

# Start the parallel plan sessions as Claude Code background sessions.
sessions:
	scripts/start-sessions.sh

# Install the launcher, ~/.mantle/src and a first mantle-ui build (plan 10).
install:
	scripts/install.sh
