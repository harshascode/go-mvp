# YouTube Download API

HTTP API to download YouTube videos as MP4 or MP3 using `yt-dlp` + `ffmpeg`.

## Run locally

```bash
go run ./main
```

Needs `yt-dlp` and `ffmpeg` installed.

## Docker

```bash
docker build -t youtube-download-api .
docker run --rm -p 8080:8080 youtube-download-api
```

## API

```text
GET /download?url=YOUTUBE_URL
```

Optional query params:

- `format`: `mp4` or `mp3`, default `mp4`
- `quality`: video quality from `144` to `4320`, default `1080`

Examples:

```bash
curl -L "http://localhost:8080/download?url=https://www.youtube.com/watch?v=VIDEO_ID" -o video.mp4
curl -L "http://localhost:8080/download?url=https://www.youtube.com/watch?v=VIDEO_ID&format=mp3" -o audio.mp3
curl -L "http://localhost:8080/download?url=https://www.youtube.com/watch?v=VIDEO_ID&quality=720" -o video.mp4
```

Health check:

```text
GET /healthz
```

## Deploy config

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address |
| `YTDLP_BIN` | `yt-dlp` | yt-dlp binary path |
| `TEMP_DIR` | system temp dir | Download work directory |
| `DOWNLOAD_TIMEOUT` | `20m` | Max time for one download |
| `MAX_CONCURRENT_DOWNLOADS` | `2` | Global concurrent downloads |
| `RATE_LIMIT_PER_MINUTE` | `10` | Per-IP request limit |
| `MAX_FILE_SIZE_MB` | `1024` | yt-dlp max file size |
| `MAX_VIDEO_DURATION` | `2h` | Max YouTube video duration |
| `TEMP_FILE_MAX_AGE` | `2h` | Remove stale temp dirs older than this |
| `CLEANUP_INTERVAL` | `15m` | Temp cleanup interval |
| `ALLOWED_ORIGIN` | `*` | CORS allowed origin |

## Production notes

Recommended:

- Put behind Nginx/Caddy/Cloudflare.
- Set `MAX_CONCURRENT_DOWNLOADS` based on server CPU/RAM/bandwidth.
- Keep `RATE_LIMIT_PER_MINUTE` enabled if public.
- Use a temp disk with enough free space.
- Check logs with `docker logs -f <container>`.
