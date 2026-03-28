FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates ffmpeg yt-dlp

WORKDIR /app

ENV ADDR=:8080 \
    TEMP_DIR=/tmp/cobalt-go-mvp

COPY --from=build /out/server /usr/local/bin/server

EXPOSE 8080

CMD ["server"]
