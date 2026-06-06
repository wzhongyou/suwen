.PHONY: build test run clean

# Binary name
BINARY = suwen

# Build flags
LDFLAGS = -s -w

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/suwen/

test:
	go test ./... -v -count=1

test-race:
	go test ./... -race -count=1

run:
	go run ./cmd/suwen/ --config=conf/suwen.toml

clean:
	rm -f $(BINARY)
	go clean -cache

fmt:
	go fmt ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
