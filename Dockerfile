# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o bot ./cmd/bot

# Stage 2: Runtime image
FROM node:20-alpine

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    sqlite \
    git \
    curl \
    kubectl \
    helm

# Install claude-code CLI
RUN npm install -g @anthropic-ai/claude-code

# Create app directory
WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/bot /usr/local/bin/bot

# Copy configuration and migrations
COPY configs /app/configs
COPY migrations /app/migrations

# Create directories for data
RUN mkdir -p /data /workspaces

# Create non-root user
RUN adduser -D -u 1000 botuser && \
    chown -R botuser:botuser /data /workspaces /app

# Switch to non-root user
USER botuser

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD pgrep -f bot || exit 1

# Run the bot
ENTRYPOINT ["/usr/local/bin/bot"]
