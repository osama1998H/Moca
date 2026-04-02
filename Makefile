# MOCA Framework Makefile
# ========================

# Build variables
VERSION    ?= dev
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)

GO := go
BENCH_PKGS := ./pkg/meta ./pkg/document ./pkg/orm ./pkg/api ./pkg/hooks

.PHONY: build build-server build-worker build-scheduler build-moca build-outbox \
        test test-integration test-api-integration lint clean \
        bench bench-integration bench-compare bench-save-baseline bench-profile \
        spike-pg spike-redis spike-gowork spike-meili spike-cobra \
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
	@echo "  bench            Run Tier 1 benchmarks"
	@echo "  bench-integration Run Docker-backed Tier 1 integration benchmarks"
	@echo "  bench-compare    Compare current benchmark run against bench-baseline.txt"
	@echo "  bench-save-baseline Save the latest benchmark run as bench-baseline.txt"
	@echo "  bench-profile    Capture CPU and memory profiles for a benchmark"
	@echo "  lint             Run golangci-lint"
	@echo "  clean            Remove build artifacts"
	@echo ""
	@echo "  spike-pg         Run PostgreSQL tenant isolation spike (MS-00-T2)"
	@echo "  spike-redis      Run Redis Streams consumer group spike (MS-00-T3)"
	@echo "  spike-gowork     Run Go workspace composition spike (MS-00-T4)"
	@echo "  spike-meili      Run Meilisearch tenant isolation spike (MS-00-T5)"
	@echo "  spike-cobra      Run Cobra CLI extension spike (MS-00-T4)"
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
	docker compose up -d --wait && \
	$(GO) test -race -count=1 -tags=integration ./... ; \
	docker compose down

## test-api-integration: Run API integration tests against real PG + Redis
test-api-integration:
	docker compose up -d --wait && \
	$(GO) test -race -count=1 -tags=integration ./pkg/api/... ; \
	docker compose down

## bench: Run Tier 1 benchmarks without Docker
bench:
	bash -o pipefail -ec '$(GO) test -run=^$$ -bench=. -benchmem -count=5 -timeout=10m $(BENCH_PKGS) | tee bench-latest.txt'

## bench-integration: Run Docker-backed Tier 1 integration benchmarks
bench-integration:
	bash -o pipefail -ec 'trap "docker compose down" EXIT; \
		docker compose up -d --wait; \
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

## spike-pg: Run the PostgreSQL schema-per-tenant spike
spike-pg:
	cd spikes/pg-tenant && $(GO) test -v -count=1 ./...

## spike-redis: Run the Redis Streams consumer group spike
spike-redis:
	cd spikes/redis-streams && GOWORK=off $(GO) test -v -count=1 ./...

## spike-gowork: Run the Go workspace composition spike
spike-gowork:
	cd spikes/go-workspace && $(GO) test -v -count=1 ./...

## spike-meili: Run the Meilisearch tenant isolation spike
spike-meili:
	cd spikes/meilisearch && GOWORK=off $(GO) test -v -count=1 ./...

## spike-cobra: Run the Cobra CLI extension spike
spike-cobra:
	cd spikes/cobra-ext && $(GO) test -v -count=1 ./...
