# syntax=docker/dockerfile:1

FROM golang:1.27-alpine3.24 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /discord-bot .

FROM alpine:3.24
# ca-certificates for TLS to Discord/Last.fm/Spotify/AWS; tzdata for DIGEST_TIMEZONE.
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /discord-bot /discord-bot

# Health endpoint (GET /healthz). Set HEALTH_PORT=off to disable.
EXPOSE 8080
HEALTHCHECK --interval=1m --timeout=10s --start-period=2m --retries=3 CMD ["/discord-bot", "healthcheck"]

# SIGTERM triggers a graceful shutdown.
STOPSIGNAL SIGTERM
ENTRYPOINT ["/discord-bot"]
