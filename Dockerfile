# Multi-stage Dockerfile for Shopify Checkout Engine (Optimized for Render / Docker)
# Stage 1: Build binary
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies & ca-certificates
RUN apk add --no-cache ca-certificates git

# Cache modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Compile statically linked binary with stripped debug symbols
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o shopapi .

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
