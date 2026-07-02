.PHONY: demo install generate run clean test

# Complete demo workflow
demo:
	@rm -rf api docs
	@echo "Running Enum Params demo..."
	@$(MAKE) generate
	@echo ""
	@echo "Demo ready! Starting server..."
	@echo ""
	@$(MAKE) run

# Install required tools
install:
	@echo "Installing onekit plugins..."
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@GOPROXY=direct go install github.com/1homsi/onekit/cmd/protoc-gen-onekit-go-http@latest
	@GOPROXY=direct go install github.com/1homsi/onekit/cmd/protoc-gen-onekit-go-client@latest
	@GOPROXY=direct go install github.com/1homsi/onekit/cmd/protoc-gen-onekit-openapiv3@latest
	@echo "Tools installed"

# Generate code from proto files
generate:
	@echo "Fetching dependencies..."
	@buf dep update
	@echo "Generating code..."
	@buf generate
	@echo "Updating Go modules..."
	@go mod tidy
	@echo "Code generated and dependencies updated"

# Run the server
run: generate
	@go run main.go

# Clean generated files
clean:
	@rm -rf api/ docs/
	@echo "Cleaned generated files"

# Automated regression test for enum query/path params (#151)
test:
	@bash test_enum_params.sh
