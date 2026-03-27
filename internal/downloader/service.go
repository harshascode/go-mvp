package downloader

import (
	"net/url"
	"strings"
)

func SupportedService(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")

	switch {
	case host == "youtube.com" || host == "youtu.be" || host == "m.youtube.com" || host == "music.youtube.com":
		return "youtube", true
	case host == "instagram.com":
		return "instagram", true
	case host == "pin.it" || host == "pinterest.com":
		return "pinterest", true
	case host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com"):
		return "tiktok", true
	default:
		return "", false
	}
}

func ServiceFromExtractor(extractor string) (string, bool) {
	extractor = strings.ToLower(extractor)

	switch {
	case strings.Contains(extractor, "youtube"):
		return "youtube", true
	case strings.Contains(extractor, "instagram"):
		return "instagram", true
	case strings.Contains(extractor, "pinterest"):
		return "pinterest", true
	case strings.Contains(extractor, "tiktok"):
		return "tiktok", true
	default:
		return "", false
	}
}
