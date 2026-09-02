# syntax=docker/dockerfile:1
# Build stage
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gdrive-bot ./cmd/gdrive-bot

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata yt-dlp \
    && addgroup -S bot && adduser -S bot -G bot \
    && mkdir -p /data /downloads && chown -R bot:bot /data /downloads
USER bot
WORKDIR /app
COPY --from=build /out/gdrive-bot /app/gdrive-bot
ENV DATA_DIR=/data \
    DOWNLOAD_DIRECTORY=/downloads \
    YTDLP_BIN=/usr/bin/yt-dlp
VOLUME ["/data", "/downloads"]
ENTRYPOINT ["/app/gdrive-bot"]
