# syntax=docker/dockerfile:1.7
# E11 Compose — rosshield 단일 바이너리 + alpine runtime.
# Web Console 정적 자산은 git 커밋된 internal/web/dist를 그대로 사용 (Stage E10 Stage D 결정).

ARG GO_VERSION=1.26
ARG ALPINE_VERSION=3.20

# === Stage 1: Go build ===
FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/rosshield-server ./cmd/rosshield-server

# === Stage 2: runtime ===
FROM alpine:${ALPINE_VERSION}
RUN apk add --no-cache ca-certificates wget tzdata && \
    addgroup -S rosshield && adduser -S -G rosshield -h /var/lib/rosshield rosshield && \
    mkdir -p /var/lib/rosshield && chown -R rosshield:rosshield /var/lib/rosshield

COPY --from=builder /out/rosshield-server /usr/local/bin/rosshield-server
COPY deploy/compose/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

USER rosshield
WORKDIR /var/lib/rosshield
EXPOSE 8080

ENV ROSSHIELD_DATA_DIR=/var/lib/rosshield
ENV ROSSHIELD_LISTEN_ADDR=0.0.0.0:8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["rosshield-server"]
