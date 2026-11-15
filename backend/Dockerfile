# ── Build stage ──────────────────────────────────────────────────────
FROM golang:1.25 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/agent       ./cmd/agent
RUN CGO_ENABLED=0 go build -o /bin/alertworker  ./cmd/alertworker

# ── Runtime stage ────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12

COPY --from=builder /bin/agent       /bin/agent
COPY --from=builder /bin/alertworker /bin/alertworker

# Default entrypoint is the ingest/API agent.
# The alert-worker service overrides this with ["/bin/alertworker"].
ENTRYPOINT ["/bin/agent"]
