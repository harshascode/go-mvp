FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY main ./main
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./main

FROM alpine:3.22

RUN apk add --no-cache ca-certificates ffmpeg yt-dlp

ENV ADDR=:8080 \
    TEMP_DIR=/tmp/yt-download-api \
    MAX_CONCURRENT_DOWNLOADS=2 \
    RATE_LIMIT_PER_MINUTE=10 \
    DOWNLOAD_TIMEOUT=20m \
    MAX_FILE_SIZE_MB=1024 \
    MAX_VIDEO_DURATION=2h \
    TEMP_FILE_MAX_AGE=2h \
    CLEANUP_INTERVAL=15m \
    ALLOWED_ORIGIN=*

COPY --from=build /out/server /usr/local/bin/server

EXPOSE 8080

CMD ["server"]
