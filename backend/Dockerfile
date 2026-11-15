# Multi-stage build for EzMonitor backend
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install git and build essentials
RUN apk add --no-cache git ca-certificates

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server and alertworker binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/alertworker ./cmd/alertworker

# Final runtime image
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/server /app/server
COPY --from=builder /app/server /bin/server
COPY --from=builder /app/alertworker /bin/alertworker

EXPOSE 8080

ENTRYPOINT ["/app/server"]

