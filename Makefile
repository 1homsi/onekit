BIN_DIR := ./bin
CMD_DIR := ./cmd
SCRIPTS_DIR := ./scripts
GO_BUILD_FLAGS ?=
GOLANGCI_LINT_VERSION := v2.11.3
GO_TEST_COVERAGE_VERSION := v2.8.3
ACTIONLINT_VERSION := v1.7.9

CMD_DIRS := $(wildcard $(CMD_DIR)/*)
BINARIES := $(notdir $(CMD_DIRS))
BINARY_PATHS := $(addprefix $(BIN_DIR)/, $(BINARIES))
GO_SOURCES := $(shell find $(CMD_DIR) internal -name '*.go')

.PHONY: all
all: help

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build            Build all generator binaries into ./bin"
	@echo "  clean            Remove built binaries"
	@echo "  fmt              Format Go code"
	@echo "  fmt-schema       Format .onk schema files"
	@echo "  install          Install local development tools"
	@echo "  install-binaries Install generators to GOPATH/bin"
	@echo "  lint             Run golangci-lint"
	@echo "  lint-fix         Run golangci-lint with auto-fix"
	@echo "  list-binaries    Show generator binary targets"
	@echo "  test             Run full test script"
	@echo "  test-fast        Run fast test script"
	@echo "  check-generated Verify committed example output is reproducible"
	@echo "  check-schema     Verify .onk schema formatting and semantics"
	@echo ""
	@echo "Generators: $(BINARIES)"

.PHONY: build
build: $(BINARY_PATHS)

$(BIN_DIR)/%: $(GO_SOURCES) go.mod go.sum | $(BIN_DIR)
	@echo "Building $*..."
	@go build $(GO_BUILD_FLAGS) -o $@ ./$(CMD_DIR)/$*

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

.PHONY: clean
clean:
	@echo "Cleaning built binaries..."
	@rm -rf $(BIN_DIR)

.PHONY: test
test: check-scripts
	@echo "Running tests with coverage analysis..."
	@$(SCRIPTS_DIR)/run_tests.sh

.PHONY: test-fast
test-fast: check-scripts
	@echo "Running tests in fast mode..."
	@$(SCRIPTS_DIR)/run_tests.sh --fast

.PHONY: install
install:
	@echo "Installing required dependencies..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
		echo "OK: golangci-lint installed"; \
	else \
		echo "OK: golangci-lint already installed"; \
	fi
	@if ! command -v go-test-coverage >/dev/null 2>&1; then \
		go install github.com/vladopajic/go-test-coverage/v2@$(GO_TEST_COVERAGE_VERSION); \
		echo "OK: go-test-coverage installed"; \
	else \
		echo "OK: go-test-coverage already installed"; \
	fi
	@if ! command -v actionlint >/dev/null 2>&1; then \
		go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION); \
		echo "OK: actionlint installed"; \
	else \
		echo "OK: actionlint already installed"; \
	fi

.PHONY: install-binaries
install-binaries:
	@echo "Installing generators to GOPATH/bin..."
	@for binary in $(BINARIES); do \
		echo "Installing $$binary..."; \
		go install ./$(CMD_DIR)/$$binary; \
	done

.PHONY: fmt
fmt:
	@echo "Formatting Go code..."
	@go fmt ./...

.PHONY: fmt-schema
fmt-schema: build
	@./bin/onek fmt

.PHONY: check-schema
check-schema: build
	@./bin/onek fmt --check
	@./bin/onek check

.PHONY: lint
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Run 'make install' first."; \
		exit 1; \
	fi

.PHONY: lint-fix
lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix; \
	else \
		echo "golangci-lint not found. Run 'make install' first."; \
		exit 1; \
	fi

.PHONY: rebuild
rebuild: clean build

.PHONY: list-binaries
list-binaries:
	@for binary in $(BINARIES); do \
		echo "$(BIN_DIR)/$$binary"; \
	done

.PHONY: check-scripts
check-scripts:
	@if [ ! -x "$(SCRIPTS_DIR)/run_tests.sh" ]; then \
		chmod +x $(SCRIPTS_DIR)/run_tests.sh; \
	fi

.PHONY: ci-validate
ci-validate:
	@echo "Validating GitHub Actions workflows..."
	@if ! command -v actionlint >/dev/null 2>&1; then \
		echo "actionlint not found. Run 'make install' first."; \
		exit 1; \
	fi
	@actionlint .github/workflows/*.yml

.PHONY: check-generated
check-generated: build
	@./bin/onek build examples/onk-simple-api
	@git diff --exit-code -- examples/onk-simple-api/api examples/onk-simple-api/docs
