.PHONY: build test run clean fmt lint tidy dev all

# Binary name
BINARY = suwen

# Build flags
LDFLAGS = -s -w

all: fmt lint test build ## Run all checks and build

build: ## Build the production binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/suwen/

dev: ## Start development server with hot reload (requires air)
	air --build.cmd "go build -ldflags '$(LDFLAGS)' -o ./tmp/$(BINARY) ./cmd/suwen/" --build.bin "./tmp/$(BINARY) --config=conf/suwen.toml"

run: ## Start the server
	go run ./cmd/suwen/ --config=conf/suwen.toml

test: ## Run all tests
	go test ./... -v -count=1

test-race: ## Run tests with race detector
	go test ./... -race -count=1

test-cover: ## Run tests with coverage
	go test ./... -coverprofile=coverage.out -count=1
	go tool cover -func=coverage.out

fmt: ## Format Go code
	go fmt ./...

lint: ## Run golangci-lint (requires golangci-lint)
	golangci-lint run ./...

tidy: ## Tidy Go module dependencies
	go mod tidy

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out
	go clean -cache

# ---- Frontend ----

web-install: ## Install frontend dependencies
	cd web && npm ci

web-dev: ## Start frontend dev server
	cd web && npm run dev

web-build: ## Build frontend for production
	cd web && npm run build

# ---- Docker ----

docker-build: ## Build Docker image
	docker build -t suwen:latest .

docker-up: ## Start all services with Docker Compose
	docker compose up -d

docker-down: ## Stop Docker Compose services
	docker compose down

# ---- Help ----

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
