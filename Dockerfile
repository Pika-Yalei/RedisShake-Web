FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends curl git openssl && rm -rf /var/lib/apt/lists/*
COPY . .
RUN bash scripts/build.sh "$TARGETOS" "$TARGETARCH" /out

FROM debian:bookworm-slim
RUN groupadd -g 10001 app && useradd -u 10001 -g 10001 -m app && mkdir -p /app /data /run/redisshake && chown -R app:app /app /data /run/redisshake
COPY --from=builder --chown=app:app /out/ /app/
USER app
WORKDIR /app
ENTRYPOINT ["/app/redis-shake-web"]
CMD ["serve", "--data-dir", "/data", "--socket-dir", "/run/redisshake", "--listen", "0.0.0.0:8080", "--redis-shake", "/app/redis-shake"]
