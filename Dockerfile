# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26.2

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

RUN GOBIN=/out go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/api \
    ./cmd/api

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/worker \
    ./cmd/worker

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/schema \
    ./cmd/schema

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app \
    && mkdir -p /app/environment /app/logs /app/migrations \
    && chown -R app:app /app

WORKDIR /app

COPY --from=builder --chown=app:app /out/api /app/api
COPY --from=builder --chown=app:app /out/worker /app/worker
COPY --from=builder --chown=app:app /out/schema /app/schema
COPY --from=builder --chown=app:app /out/goose /usr/local/bin/goose
COPY --chown=app:app environment/docker.yaml /app/environment/production.yaml
COPY --chown=app:app internal/infrastructure/mysql/migrations /app/migrations

USER app

EXPOSE 8080

CMD ["/app/api"]
