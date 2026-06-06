# ---- Build stage ----
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Build the binary.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/suwen ./cmd/suwen/

# ---- Runtime stage ----
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user.
RUN adduser -D -h /app suwen
USER suwen
WORKDIR /app

COPY --from=builder /bin/suwen /app/suwen
COPY conf/ /app/conf/

EXPOSE 8080

ENTRYPOINT ["/app/suwen"]
CMD ["--config=/app/conf/suwen.toml"]
