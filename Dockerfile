# Moca multi-target Dockerfile
#
# Build a specific binary target:
#   docker build --target moca-server -t moca-server:latest .
#   docker build --target moca-worker -t moca-worker:latest .
#   docker build --target moca-scheduler -t moca-scheduler:latest .
#   docker build --target moca-outbox -t moca-outbox:latest .
#   docker build --target moca -t moca:latest .
#
# Pass build metadata:
#   docker build --target moca-server \
#     --build-arg VERSION=1.0.0 \
#     --build-arg COMMIT=abc1234 \
#     --build-arg BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
#     -t moca-server:1.0.0 .

# ---------------------------------------------------------------------------
# Builder stage
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

# Copy dependency manifests first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 go build \
      -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/moca ./cmd/moca && \
    CGO_ENABLED=0 go build \
      -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/moca-server ./cmd/moca-server && \
    CGO_ENABLED=0 go build \
      -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/moca-worker ./cmd/moca-worker && \
    CGO_ENABLED=0 go build \
      -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/moca-scheduler ./cmd/moca-scheduler && \
    CGO_ENABLED=0 go build \
      -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildDate=${BUILD_DATE}" \
      -o /out/moca-outbox ./cmd/moca-outbox

# ---------------------------------------------------------------------------
# moca (CLI)
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca CLI — manage sites, apps, and framework commands"

COPY --from=builder /out/moca /moca

ENTRYPOINT ["/moca"]

# ---------------------------------------------------------------------------
# moca-server
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-server

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca HTTP and WebSocket server"

COPY --from=builder /out/moca-server /moca-server

EXPOSE 8000

ENTRYPOINT ["/moca-server"]

# ---------------------------------------------------------------------------
# moca-worker
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-worker

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca background job consumer (Redis Streams)"

COPY --from=builder /out/moca-worker /moca-worker

ENTRYPOINT ["/moca-worker"]

# ---------------------------------------------------------------------------
# moca-scheduler
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-scheduler

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca cron scheduler"

COPY --from=builder /out/moca-scheduler /moca-scheduler

ENTRYPOINT ["/moca-scheduler"]

# ---------------------------------------------------------------------------
# moca-outbox
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12 AS moca-outbox

LABEL org.opencontainers.image.source="https://github.com/osama1998H/Moca"
LABEL org.opencontainers.image.description="Moca transactional outbox poller (DB to Kafka)"

COPY --from=builder /out/moca-outbox /moca-outbox

ENTRYPOINT ["/moca-outbox"]
