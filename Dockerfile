FROM golang:1.22 AS builder

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN --mount=type=cache,target=/var/cache/go go env -w GOCACHE=/go-cache && \
    go env -w GOMODCACHE=/gomod-cache && \
    CGO_ENABLED=1 go build -v --tags "fts5" -o /usr/local/bin/easysearch ./...

FROM debian:bookworm-slim AS runner

RUN apt-get update \
    && apt-get install -y ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /var/run/easysearch
COPY --from=builder /usr/local/bin/easysearch /var/run/easysearch/

CMD ["./easysearch"]