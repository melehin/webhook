# Build stage
FROM golang:1.20-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook-executor

# Final stage
FROM alpine:3.18

WORKDIR /app

# Copy binary and config from builder
COPY --from=builder /app/webhook-executor .
COPY config.yaml .

# Create directory for scripts and set permissions
RUN mkdir -p /scripts && \
    chown -R nobody:nobody /scripts && \
    chown -R nobody:nobody /app

USER nobody

EXPOSE 9000

ENTRYPOINT ["./webhook-executor"]