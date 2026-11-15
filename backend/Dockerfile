# EzMonitor backend image: API server + alert worker.
# The host agent (cmd/agent) is NOT built here: it runs on each monitored
# machine, not in a container.
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/server      ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/alertworker ./cmd/alertworker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata   # CA roots for HTTPS webhooks
COPY --from=builder /out/server      /app/server
COPY --from=builder /out/alertworker /bin/alertworker
EXPOSE 8080
# Default is the API server; the alert-worker service overrides the entrypoint.
ENTRYPOINT ["/app/server"]
