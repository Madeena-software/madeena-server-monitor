# ---- Build stage ----
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/monitor ./cmd/monitor

# ---- Runtime stage ----
FROM alpine:3.20

# Install CA certificates for SMTP TLS and smartmontools (optional)
RUN apk add --no-cache ca-certificates smartmontools && \
    addgroup -S monitor && \
    adduser -S monitor -G monitor

WORKDIR /app

COPY --from=builder /bin/monitor /app/monitor

# .env is expected to be mounted at runtime (not baked in)
USER monitor

ENTRYPOINT ["/app/monitor"]
