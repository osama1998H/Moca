# MOCA Framework Makefile
# ========================

# Build variables
VERSION    ?= dev
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)

GO := go
COMPOSE := $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo "docker-compose")
BENCH_PKGS := ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks ./internal/drivers

.PHONY: build build-server build-worker build-scheduler build-moca build-outbox \
        test test-integration test-api-integration lint clean release-local \
        bench bench-integration bench-compare bench-save-baseline bench-profile \
        docs-generate docs-generate-cli docs-generate-api \
        audit audit-go audit-desk audit-go-baseline audit-desk-baseline \
        help

## help: Show available targets
help:
	@echo "MOCA Framework — available targets:"
	@echo ""
	@echo "  build            Build all 5 binaries to bin/"
	@echo "  build-server     Build moca-server binary"
	@echo "  build-worker     Build moca-worker binary"
	@echo "  build-scheduler  Build moca-scheduler binary"
	@echo "  build-moca       Build moca CLI binary"
	@echo "  build-outbox     Build moca-outbox binary"
	@echo "  test             Run all tests with race detector"
	@echo "  test-integration Run integration tests (requires Docker)"
	@echo "  test-api-integration Run API integration tests (requires Docker)"
	@echo "  bench            Run benchmarks (no Docker)"
	@echo "  bench-integration Run all benchmarks including integration (Docker required)"
	@echo "  bench-compare    Compare current benchmark run against bench-baseline.txt"
	@echo "  bench-save-baseline Save the latest benchmark run as bench-baseline.txt"
	@echo "  bench-profile    Capture CPU and memory profiles for a benchmark"
	@echo "  lint             Run golangci-lint"
	@echo "  clean            Remove build artifacts"
	@echo "  release-local    Build release archives locally (GoReleaser snapshot)"
	@echo ""
	@echo "  docs-generate    Generate CLI + API reference into wiki/"
	@echo "  docs-generate-cli Generate CLI reference only"
	@echo "  docs-generate-api Generate API reference only"
	@echo ""
	@echo "  audit            Run Skylos audit across Go backend and desk/"
	@echo "  audit-go         Run Skylos audit on pkg/, cmd/, internal/ against baseline"
	@echo "  audit-desk       Run Skylos audit on desk/ against baseline"
	@echo "  audit-go-baseline   Re-snapshot the Go Skylos baseline (accept current debt)"
	@echo "  audit-desk-baseline Re-snapshot the desk/ Skylos baseline (accept current debt)"
	@echo ""
	@echo "Override build vars: make build VERSION=0.1.0"

## build: Build all 5 binaries to bin/
build: build-server build-worker build-scheduler build-moca build-outbox

## build-server: Build the moca-server binary
build-server:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/moca-server ./cmd/moca-server

## build-worker: Build the moca-worker binary
build-worker:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/moca-worker ./cmd/moca-worker

## build-scheduler: Build the moca-scheduler binary
build-scheduler:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/moca-scheduler ./cmd/moca-scheduler

## build-moca: Build the moca CLI binary
build-moca:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/moca ./cmd/moca

## build-outbox: Build the moca-outbox binary
build-outbox:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/moca-outbox ./cmd/moca-outbox

## test: Run all tests with race detection
test:
	$(GO) test -race -count=1 ./...

## test-integration: Run integration tests against real PG + Redis (requires Docker)
test-integration:
	$(COMPOSE) up -d --wait && \
	$(GO) test -race -count=1 -tags=integration ./... ; \
	$(COMPOSE) down

## test-api-integration: Run API integration tests against real PG + Redis
test-api-integration:
	$(COMPOSE) up -d --wait && \
	$(GO) test -race -count=1 -tags=integration ./pkg/api/... ; \
	$(COMPOSE) down

## bench: Run benchmarks (no Docker)
bench:
	bash -o pipefail -ec '$(GO) test -run=^$$ -bench=. -benchmem -count=5 -timeout=10m $(BENCH_PKGS) | tee bench-latest.txt'

## bench-integration: Run all benchmarks including integration (Docker required)
bench-integration:
	bash -o pipefail -ec 'trap "$(COMPOSE) down" EXIT; \
		$(COMPOSE) up -d --wait; \
		$(GO) test -run=^$$ -tags=integration -bench=. -benchmem -count=10 -timeout=20m $(BENCH_PKGS) | tee bench-latest.txt'

## bench-compare: Compare current results against a saved baseline
bench-compare: bench
	@if [ ! -f bench-baseline.txt ]; then \
		echo "No baseline found. Run 'make bench-save-baseline' first."; \
		exit 1; \
	fi
	benchstat bench-baseline.txt bench-latest.txt

## bench-save-baseline: Save the latest benchmark run as the comparison baseline
bench-save-baseline: bench
	cp bench-latest.txt bench-baseline.txt
	@echo "Baseline saved to bench-baseline.txt"

## bench-profile: Capture CPU and memory profiles for a benchmark
bench-profile:
	@printf "Benchmark pattern (e.g. BenchmarkDocManagerInsert): "; \
	read PATTERN; \
	printf "Package (e.g. ./pkg/document): "; \
	read PKG; \
	$(GO) test -run=^$$ -bench=$$PATTERN -cpuprofile=cpu.prof -memprofile=mem.prof -benchmem $$PKG; \
	echo "Profiles saved: cpu.prof, mem.prof"; \
	echo "View with: go tool pprof -http=:8080 cpu.prof"

## lint: Run golangci-lint (requires golangci-lint to be installed)
lint:
	golangci-lint run --timeout=5m

## clean: Remove build artifacts and Go caches
clean:
	rm -rf bin/
	$(GO) clean -cache -testcache

## release-local: Build release archives locally using GoReleaser (snapshot mode)
release-local:
	goreleaser build --snapshot --clean

## docs-generate: Generate CLI + API reference into wiki/
docs-generate:
	$(GO) run ./cmd/moca docgen all --wiki-dir wiki/

## docs-generate-cli: Generate CLI reference only
docs-generate-cli:
	$(GO) run ./cmd/moca docgen cli --wiki-dir wiki/

## docs-generate-api: Generate API reference only
docs-generate-api:
	$(GO) run ./cmd/moca docgen api --wiki-dir wiki/

# --- Skylos static analysis -------------------------------------------------
# Skylos (https://github.com/duriantaco/skylos) scans for dead code, secrets,
# and risky flows. Install once with: pipx install skylos
#
# Skylos writes baselines to <scanned-path>/.skylos/baseline.json. We scan
# the repo root with exclusions for the Go pass (so one baseline covers
# pkg/, cmd/, internal/) and `desk/` from inside the submodule (its baseline
# lives in the submodule repo). Tracking the baseline in git accepts existing
# debt and gates only new regressions.

SKYLOS ?= skylos
# Exclusions for the repo-root Go scan: skip the desk/ submodule (scanned
# separately), the wiki/ submodule, vendored/build output, and node_modules.
SKYLOS_GO_EXCLUDES := \
	--exclude-folder desk \
	--exclude-folder wiki \
	--exclude-folder apps \
	--exclude-folder bin \
	--exclude-folder dist \
	--exclude-folder node_modules \
	--exclude-folder .worktrees
SKYLOS_DESK_EXCLUDES := \
	--exclude-folder node_modules \
	--exclude-folder dist \
	--exclude-folder .vite

## audit: Run Skylos across the Go backend and the desk/ submodule
audit: audit-go audit-desk

## audit-go: Scan the Go backend against the committed Go baseline
audit-go:
	$(SKYLOS) . -a $(SKYLOS_GO_EXCLUDES) --baseline

## audit-desk: Scan the desk/ submodule against its committed baseline
audit-desk:
	cd desk && $(SKYLOS) . -a $(SKYLOS_DESK_EXCLUDES) --baseline

## audit-go-baseline: Snapshot current Go findings as the new baseline
audit-go-baseline:
	$(SKYLOS) baseline . $(SKYLOS_GO_EXCLUDES)

## audit-desk-baseline: Snapshot current desk/ findings as the new baseline
audit-desk-baseline:
	cd desk && $(SKYLOS) baseline . $(SKYLOS_DESK_EXCLUDES)

