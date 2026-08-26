# goplint - a go/analysis linter enforcing DDD Value Type conventions,
# with a formally-gated soundness assurance harness.

GOCMD ?= go
GOBUILD := $(GOCMD) build
BUILD_DIR := bin
GOLANGCI_LINT := GO_CMD="$(GOCMD)" ./scripts/golangci-lint.sh

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

# ---------------------------------------------------------------------------
# Lint (normalized golangci-lint gate)
# ---------------------------------------------------------------------------

.PHONY: lint lint-fmt lint-config-verify lint-linters
lint: lint-config-verify lint-fmt
	$(GOLANGCI_LINT) run

lint-fmt:
	$(GOLANGCI_LINT) fmt

lint-config-verify:
	$(GOLANGCI_LINT) config-verify

lint-linters:
	$(GOLANGCI_LINT) linters

# ---------------------------------------------------------------------------
# Build and test
# ---------------------------------------------------------------------------

.PHONY: build-goplint
build-goplint: $(BUILD_DIR)
	@echo "Building goplint..."
	$(GOBUILD) -o $(BUILD_DIR)/goplint .

.PHONY: test
test:
	$(GOCMD) test -count=1 -timeout=25m ./...

# ---------------------------------------------------------------------------
# Self-scan (dogfood): goplint applied to its own module
# ---------------------------------------------------------------------------

.PHONY: check-types check-types-json check-types-all check-types-all-json
check-types: build-goplint
	@echo "Checking primitive type usage..."
	./$(BUILD_DIR)/goplint -config=exceptions.toml ./...

check-types-json: build-goplint
	./$(BUILD_DIR)/goplint -json -config=exceptions.toml ./... 2>/dev/null || true

check-types-all: build-goplint
	@echo "Checking DDD type compliance (all modes)..."
	./$(BUILD_DIR)/goplint -check-all -config=exceptions.toml ./...

check-types-all-json: build-goplint
	./$(BUILD_DIR)/goplint -check-all -json -config=exceptions.toml ./... 2>/dev/null || true

# ---------------------------------------------------------------------------
# Soundness-assurance gates
# ---------------------------------------------------------------------------

.PHONY: check-goplint-soundness check-goplint-soundness-routed check-goplint-docs check-goplint-soundness-consumer check-goplint-soundness-harness check-goplint-harness-parity check-goplint-module-tests check-goplint-soundness-core check-goplint-soundness-semantic check-goplint-soundness-complete generate-goplint-clean-tree-evidence rebind-goplint-clean-tree-evidence check-goplint-clean-tree-evidence check-goplint-mutation-kernel-coverage check-goplint-gate-contract check-goplint-production-integration check-goplint-counterexamples check-goplint-architecture check-goplint-catalog check-semantic-spec check-goplint-protocol-oracle check-goplint-protocol-oracle-scheduled check-goplint-end-to-end-oracle check-goplint-fuzz-seeds check-goplint-fuzz-scheduled check-goplint-targeted-mutation check-goplint-determinism check-cfg-refinement check-goplint-race-repeat update-goplint-race-repeat-timings check-goplint-repository-audit check-goplint-full-scan check-goplint-performance-smoke check-goplint-benchmarks
check-goplint-soundness: check-goplint-soundness-routed

check-goplint-soundness-routed:
	./scripts/check-routed-soundness.sh

check-goplint-docs:
	$(GOCMD) run ./cmd/docs-guard -root .

check-goplint-soundness-consumer:
	$(GOCMD) run ./cmd/soundness-gate -root . -manifest spec/soundness-gate.v1.json -profile consumer

check-goplint-soundness-harness:
	$(GOCMD) run ./cmd/soundness-gate -root . -manifest spec/soundness-gate.v1.json -profile harness

check-goplint-harness-parity:
	$(GOCMD) run ./cmd/harness-parity

check-goplint-module-tests:
	./scripts/check-module-tests.sh

check-goplint-soundness-core: check-goplint-soundness-semantic

check-goplint-soundness-semantic:
	$(GOCMD) run ./cmd/soundness-gate -root . -manifest spec/soundness-gate.v1.json -profile semantic

check-goplint-soundness-complete:
	$(GOCMD) run ./cmd/soundness-gate -root . -manifest spec/soundness-gate.v1.json -profile complete

generate-goplint-clean-tree-evidence:
	$(GOCMD) run ./cmd/clean-tree-evidence -root . -paths testdata/gates/clean-tree-v5.paths -plan testdata/gates/clean-tree-v5.json -evidence testdata/gates/clean-tree-run.v5.json

rebind-goplint-clean-tree-evidence:
	$(GOCMD) run ./cmd/clean-tree-evidence -rebind -root . -paths testdata/gates/clean-tree-v5.paths -plan testdata/gates/clean-tree-v5.json -evidence testdata/gates/clean-tree-run.v5.json

check-goplint-clean-tree-evidence:
	$(GOCMD) run ./cmd/check-clean-tree-evidence -root . -paths testdata/gates/clean-tree-v5.paths -plan testdata/gates/clean-tree-v5.json -evidence testdata/gates/clean-tree-run.v5.json

check-goplint-mutation-kernel-coverage:
	$(GOCMD) run ./cmd/mutation-kernel-coverage -root . -manifest testdata/subgates/mutation-kernel-coverage.v1.json

check-goplint-gate-contract:
	./scripts/check-aggregate-contract.sh

check-goplint-production-integration:
	./scripts/check-production-integration.sh

check-goplint-counterexamples:
	./scripts/check-counterexamples.sh

check-goplint-architecture:
	./scripts/check-production-architecture.sh

check-goplint-catalog: check-semantic-spec

check-semantic-spec:
	./scripts/check-semantic-spec.sh

check-goplint-protocol-oracle:
	./scripts/check-protocol-oracle.sh

check-goplint-protocol-oracle-scheduled:
	./scripts/check-protocol-oracle-scheduled.sh

check-goplint-end-to-end-oracle:
	./scripts/check-protocol-oracle-e2e.sh

check-goplint-fuzz-seeds:
	./scripts/check-fuzz-seeds.sh

check-goplint-fuzz-scheduled:
	./scripts/check-fuzz-scheduled.sh

check-goplint-targeted-mutation:
	$(GOCMD) run ./cmd/targeted-mutation -profile testdata/mutation/profiles/blocking-v2.json

check-goplint-determinism:
	./scripts/check-protocol-determinism.sh

check-cfg-refinement:
	./scripts/check-cfg-refinement.sh

check-goplint-race-repeat:
	./scripts/check-race-repeat.sh

update-goplint-race-repeat-timings:
	$(GOCMD) run ./cmd/race-repeat-timings -samples 3 -output spec/goplint-test-timings.v1.json

check-goplint-repository-audit: build-goplint
	$(GOCMD) run ./cmd/repository-audit -mode produce
	$(GOCMD) run ./cmd/subgate-report -observation repository-scans=canonical-superset-audit

check-goplint-full-scan: build-goplint
	$(GOCMD) run ./cmd/repository-audit -mode full-scan
	$(GOCMD) run ./cmd/subgate-report -observation repository-scans=baseline-production-scan

check-goplint-performance-smoke:
	./scripts/check-performance-smoke.sh

check-goplint-benchmarks:
	./scripts/check-cfg-bench-thresholds.sh

# Check for self-scan regressions against the committed baseline.
# Reports only NEW findings not present in baseline.toml. Exit code 0 = clean.
.PHONY: check-baseline
check-baseline: build-goplint
	@echo "Checking goplint baseline..."
	$(GOCMD) run ./cmd/repository-audit -mode baseline
	$(GOCMD) run ./cmd/subgate-report -observation repository-scans=canonical-production-scan

# Audit goplint exception governance.
.PHONY: check-goplint-exceptions
check-goplint-exceptions: build-goplint
	@echo "Auditing goplint stale exceptions..."
	$(GOCMD) run ./cmd/repository-audit -mode exceptions
	@echo "Auditing goplint exception review dates..."
	$(GOCMD) run ./cmd/repository-audit -mode review-dates
	$(GOCMD) run ./cmd/subgate-report -observation repository-scans=exception-governance

# Update the self-scan baseline from the current module state.
.PHONY: update-baseline
update-baseline: build-goplint
	@echo "Updating goplint baseline..."
	./$(BUILD_DIR)/goplint -test=false -check-all -check-enum-sync -update-baseline=baseline.toml -config=exceptions.toml ./...
	@echo "Baseline updated: baseline.toml"

# Check SPDX license headers in all Go files
.PHONY: license-check
license-check:
	@echo "Checking SPDX license headers..."
	@missing=0; \
	for file in $$(find . -name "*.go" -type f); do \
		if ! head -1 "$$file" | grep -q "SPDX-License-Identifier: MPL-2.0"; then \
			echo "Missing SPDX header: $$file"; \
			missing=$$((missing + 1)); \
		fi; \
	done; \
	if [ $$missing -gt 0 ]; then \
		echo ""; \
		echo "ERROR: $$missing file(s) missing SPDX-License-Identifier: MPL-2.0 header"; \
		echo "All Go source files must start with: // SPDX-License-Identifier: MPL-2.0"; \
		exit 1; \
	else \
		echo "All Go files have proper SPDX license headers."; \
	fi

# Check that all Go files stay under the 1000-line limit
.PHONY: check-file-length
check-file-length:
	./scripts/check-file-length.sh

# Cross-compile and vet for Windows to catch platform-divergent code early
.PHONY: check-windows-build
check-windows-build:
	@echo "[1/2] Cross-compiling for windows/amd64..."
	GOOS=windows GOARCH=amd64 $(GOCMD) build ./...
	@echo "[2/2] Vetting for windows/amd64..."
	GOOS=windows GOARCH=amd64 $(GOCMD) vet ./...

# ---------------------------------------------------------------------------
# Mutation testing (advisory go-mutesting profile; blocking targeted mutation
# is a soundness subgate, see check-goplint-targeted-mutation)
# ---------------------------------------------------------------------------

MUTATION_MODULE ?= goplint
MUTATION_BASE_REF ?= origin/main
MUTATION_MODE ?= advisory
MUTATION_REPORT_DIR ?= artifacts/mutation

.PHONY: mutation-dry-run mutation-pr mutation-full mutation-baseline-update mutation-rerun
mutation-dry-run:
	@./scripts/mutation.sh dry-run --module "$(MUTATION_MODULE)" --report-dir "$(MUTATION_REPORT_DIR)"

mutation-pr:
	@./scripts/mutation.sh pr --module "$(MUTATION_MODULE)" --base "$(MUTATION_BASE_REF)" --mode "$(MUTATION_MODE)" --report-dir "$(MUTATION_REPORT_DIR)"

mutation-full:
	@./scripts/mutation.sh full --module "$(MUTATION_MODULE)" --mode "$(MUTATION_MODE)" --report-dir "$(MUTATION_REPORT_DIR)"

mutation-baseline-update:
	@./scripts/mutation.sh baseline-update --module "$(MUTATION_MODULE)" --report-dir "$(MUTATION_REPORT_DIR)"

mutation-rerun:
	@test -n "$(MUTATION_MUTANT_ID)" || { echo "MUTATION_MUTANT_ID is required"; exit 2; }
	@MUTATION_MUTANT_ID="$(MUTATION_MUTANT_ID)" ./scripts/mutation.sh rerun --module "$(MUTATION_MODULE)" --mutant-id "$(MUTATION_MUTANT_ID)" --report-dir "$(MUTATION_REPORT_DIR)"

# Lint shell scripts with shellcheck (optional tool)
SHELLCHECK := $(shell command -v shellcheck 2>/dev/null)

.PHONY: lint-scripts
lint-scripts:
	@echo "Linting shell scripts..."
ifdef SHELLCHECK
	shellcheck scripts/*.sh
else
	@echo "  (shellcheck not found, skipping shell script linting)"
endif
