FROM golang:1.23 AS builder

WORKDIR /usr/src/app

# Required for sqlite-vec to compile properly, even though it uses a portable version of SQLite at runtime
RUN apt-get update && apt-get install -y libsqlite3-dev

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN --mount=type=cache,target=/var/cache/go go env -w GOCACHE=/var/cache/go/build && \
    go env -w GOMODCACHE=/var/cache/go/gomod && \
    CGO_ENABLED=1 go build -v --tags "fts5" -o /usr/local/bin/easysearch ./app

FROM debian:bookworm-slim AS runner

RUN apt-get update \
    && apt-get install -y ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /var/run/easysearch
COPY --from=builder /usr/local/bin/easysearch /var/run/easysearch/

CMD ["./easysearch"]