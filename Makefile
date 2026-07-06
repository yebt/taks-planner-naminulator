BINARY := planner
PKG    := ./cmd/planner
BIN    := bin/$(BINARY)

.DEFAULT_GOAL := help

.PHONY: help build build-windows build-all run tui config test vet fmt tidy check clean install

help: ## List available commands
	@echo "planner — make targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Compile for the current platform into bin/
	go build -o $(BIN) $(PKG)

build-windows: ## Cross-compile a Windows amd64 .exe into bin/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BIN).exe $(PKG)

build-all: build build-windows ## Build for the current platform and Windows

run: ## Run the interactive chat harness
	go run $(PKG)

tui: ## Run the harness (alias)
	go run $(PKG) tui

config: ## Open the configuration TUI
	go run $(PKG) config

test: ## Run all tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

tidy: ## Tidy go.mod / go.sum
	go mod tidy

check: fmt vet test ## Format, vet and test

install: ## Install the binary to GOPATH/bin
	go install $(PKG)

clean: ## Remove build artifacts
	rm -rf bin
