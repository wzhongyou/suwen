# Contributing to 素问 (Suwen)

Thanks for your interest in contributing! Suwen is an open-source AI search engine written in Go.

## Getting started

### Prerequisites

- **Go** >= 1.25
- **Node.js** >= 22 (for the web frontend)
- Running instances of [Vortex](https://github.com/wzhongyou/vortex) and [Proximia](https://github.com/wzhongyou/proximia) (or use mocks for development)

### Setup

```bash
git clone https://github.com/wzhongyou/suwen.git
cd suwen

# Backend
cp conf/llmgate.toml.example conf/llmgate.toml
# Edit conf/llmgate.toml with your API keys
make build
make test

# Frontend
cd web
npm install
npm run dev
```

## Project structure

```
suwen/
├── cmd/suwen/main.go       # Entry point, middleware chain
├── internal/
│   ├── config/             # TOML configuration
│   ├── query/              # Query understanding (intent, rewriting)
│   ├── retrieval/          # Hybrid search, RRF fusion
│   ├── ranking/            # Multi-stage ranking (Cross-Encoder)
│   ├── generation/         # LLM answer synthesis
│   ├── gateway/            # HTTP API, SSE, search UI
│   ├── cache/              # Query result cache
│   └── middleware/         # Rate limit, auth, metrics, logging
├── web/                    # Next.js search console
├── conf/                   # Configuration files
└── docs/design/            # Architecture docs
```

## Development workflow

```bash
# After making changes:
make fmt        # Format code
make lint       # Run linter
make test       # Run tests
make test-race  # Run tests with race detector
make build      # Build binary
```

## Code style

- Follow standard Go conventions (`go fmt`, `go vet`).
- Package names are short and lowercase.
- Interfaces are defined where they are consumed.
- Config structs live in `internal/config/`.
- New middleware goes in `internal/middleware/`.

## Testing

- Write table-driven tests.
- Mock external services (Vortex, Proximia) where possible.
- `make test` must pass before submitting a PR.

## Commit messages

Follow [conventional commits](https://www.conventionalcommits.org/):

```
feat: add Cross-Encoder reranking
fix: handle Proximia timeout gracefully
refactor: extract RRF fusion to separate function
docs: update API documentation
```

## Questions?

Open a [discussion](https://github.com/wzhongyou/suwen/discussions) or join the issue tracker.
