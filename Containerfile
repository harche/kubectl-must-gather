# Build stage
FROM golang:1.23-alpine AS builder

# Install git and ca-certificates (needed for go modules)
RUN apk update && apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o aks-must-gather ./cmd/aks-must-gather

# Final stage
FROM alpine:latest

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/aks-must-gather .

# Change ownership to non-root user
RUN chown appuser:appgroup /app/aks-must-gather

# Switch to non-root user
USER appuser

# Set the binary as executable
RUN chmod +x /app/aks-must-gather

# Run the binary
ENTRYPOINT ["/app/aks-must-gather"]