# Go MVP

This is a separate Go-based MVP API for media downloads.

It is intentionally narrower than the JavaScript API:

- Supported services: YouTube, Instagram, TikTok, Pinterest
- Backend engine: `yt-dlp`
- Job store: in-memory
- Download flow: `POST /resolve` then `GET /download/{id}`

## Why this shape

For these four services, the hard problem is extractor reliability, not HTTP routing.
Using `yt-dlp` as the extraction and download engine is the fastest path to a working MVP in Go without rebuilding cobalt's entire service stack.

## Endpoints

- `GET /healthz`
- `POST /resolve`
- `GET /download/{id}`

### `POST /resolve`

Request:

```json
{
  "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
  "mode": "video",
  "quality": "1080",
  "format": "mp4"
}
```

`mode` can be `video` or `audio`.

For `video`:

- `quality`: `best`, `4320`, `2160`, `1440`, `1080`, `720`, `480`, `360`, `240`, `144`
- `format`: `mp4`, `webm`, `mkv`

For `audio`:

- `quality`: `best`, `320`, `256`, `192`, `128`, `96`, `64`
- `format`: `mp3`, `m4a`, `opus`, `wav`, `flac`

Response:

```json
{
  "id": "job_id",
  "service": "youtube",
  "title": "video title",
  "filename": "video title.mp4",
  "mode": "video",
  "quality": "1080",
  "format": "mp4",
  "downloadUrl": "http://localhost:8080/download/job_id",
  "expiresAt": "2026-01-01T00:00:00Z"
}
```

## Required binaries

- `yt-dlp`
- `ffmpeg` if you use `mode=audio` or formats that require merge/remuxing

## Environment

- `ADDR` default: `:8080`
- `YTDLP_BIN` default: `yt-dlp`
- `JOB_TTL` default: `30m`
- `RESOLVE_TIMEOUT` default: `20s`
- `DOWNLOAD_TIMEOUT` default: `20m`
- `MAX_CONCURRENT_DOWNLOADS` default: `2`
- `TEMP_DIR` default: system temp dir + `/cobalt-go-mvp`

## Run

```bash
cd go-mvp
go run ./cmd/server
```
