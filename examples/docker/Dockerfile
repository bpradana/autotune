# Multi-stage Dockerfile for autotune example application
FROM golang:1.21-alpine AS builder

# Install git and ca-certificates for Go modules
RUN apk add --no-cache git ca-certificates tzdata

# Create application user
RUN adduser -D -g '' autotuneuser

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o autotune-demo \
    ./examples/observability/

# Test the build
RUN go test ./...

# Production stage
FROM scratch

# Import from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd

# Copy the binary
COPY --from=builder /build/autotune-demo /autotune-demo

# Use non-root user
USER autotuneuser

# Expose ports
EXPOSE 8080 8000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/autotune-demo", "-health-check"]

# Run the application
ENTRYPOINT ["/autotune-demo"]

# Metadata
LABEL maintainer="autotune-team@example.com"
LABEL description="Autotune GC optimization demo application"
LABEL version="1.0.0"
LABEL org.opencontainers.image.source="https://github.com/bpradana/autotune"
LABEL org.opencontainers.image.documentation="https://github.com/bpradana/autotune/blob/main/README.md"
LABEL org.opencontainers.image.licenses="MIT"