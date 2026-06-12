package downloader

import (
	"net/url"
	"strings"
)

func IsYouTube(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")

	return host == "youtube.com" || host == "m.youtube.com" || host == "music.youtube.com" || host == "youtu.be"
}
