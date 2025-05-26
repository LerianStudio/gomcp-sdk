# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the main library (verification build)
RUN go build -v ./...

# Build tools using the main module
RUN ls -la tools/ && \
    go build -o /bin/mcp-validator ./tools/mcp-validator/main.go && \
    go build -o /bin/mcp-benchmark ./tools/mcp-benchmark/main.go

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

# Copy tool binaries
COPY --from=builder /bin/mcp-validator /bin/mcp-validator
COPY --from=builder /bin/mcp-benchmark /bin/mcp-benchmark

# Default to validator
CMD ["/bin/mcp-validator"]