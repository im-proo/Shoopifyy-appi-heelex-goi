# Multi-stage Dockerfile for Shopify Checkout Engine (Optimized for Render / Docker)
# Stage 1: Build binary
FROM golang:alpine AS builder

WORKDIR /app

# Copy source code and vendored dependencies
COPY . ./

# Compile statically linked binary using pre-vendored dependencies (no network required)
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o shopapi .

# Stage 2: Minimal runtime image
FROM alpine:latest

WORKDIR /app

# CA certificates for TLS requests to Shopify & payment endpoints
RUN apk add --no-cache ca-certificates tzdata

# Copy compiled executable
COPY --from=builder /app/shopapi /app/shopapi

# Render injects PORT dynamically (default 10000)
ENV PORT=10000

EXPOSE 10000 5001

ENTRYPOINT ["/app/shopapi"]
