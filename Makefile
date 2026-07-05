BIN_DIR := ./bin
CMD_DIR := ./cmd
SCRIPTS_DIR := ./scripts

CMD_DIRS := $(wildcard $(CMD_DIR)/*)
BINARIES := $(notdir $(CMD_DIRS))
BINARY_PATHS := $(addprefix $(BIN_DIR)/, $(BINARIES))

.PHONY: all
all: help

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build            Build all generator binaries into ./bin"
	@echo "  clean            Remove built binaries"
	@echo "  fmt              Format Go code"
	@echo "  install          Install local development tools"
	@echo "  install-binaries Install generators to GOPATH/bin"
	@echo "  lint             Run golangci-lint"
	@echo "  lint-fix         Run golangci-lint with auto-fix"
	@echo "  list-binaries    Show generator binary targets"
	@echo "  proto            Regenerate onekit annotation Go files"
	@echo "  publish          Publish annotations to Buf Schema Registry"
	@echo "  test             Run full test script"
	@echo "  test-fast        Run fast test script"
	@echo ""
	@echo "Generators: $(BINARIES)"

.PHONY: build
build: $(BINARY_PATHS)

$(BIN_DIR)/%: $(CMD_DIR)/%/*.go | $(BIN_DIR)
	@echo "Building $*..."
	@go build -o $@ ./$(CMD_DIR)/$*

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
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		echo "OK: golangci-lint installed"; \
	else \
		echo "OK: golangci-lint already installed"; \
	fi
	@if ! command -v go-test-coverage >/dev/null 2>&1; then \
		go install github.com/vladopajic/go-test-coverage/v2@latest; \
		echo "OK: go-test-coverage installed"; \
	else \
		echo "OK: go-test-coverage already installed"; \
	fi
	@if ! command -v actionlint >/dev/null 2>&1; then \
		go install github.com/rhysd/actionlint/cmd/actionlint@latest; \
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

.PHONY: proto
proto:
	@echo "Generating Go code from proto files..."
	@protoc --go_out=. --go_opt=module=github.com/corezio/onekit \
		--go_opt=Monekit/http/annotations.proto=github.com/corezio/onekit/http \
		--go_opt=Monekit/http/headers.proto=github.com/corezio/onekit/http \
		--go_opt=Monekit/http/errors.proto=github.com/corezio/onekit/http \
		--proto_path=. \
		proto/onekit/http/annotations.proto \
		proto/onekit/http/headers.proto \
		proto/onekit/http/errors.proto

.PHONY: publish
publish:
	@echo "Publishing annotations to Buf Schema Registry..."
	@cd proto && buf push
	@echo "OK: Published to buf.build/corezio/onekit"

.PHONY: fmt
fmt:
	@echo "Formatting Go code..."
	@go fmt ./...

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
