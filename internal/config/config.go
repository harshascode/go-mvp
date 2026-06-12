package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Addr                   string
	YTDLPBin               string
	TempDir                string
	DownloadTimeout        time.Duration
	MaxConcurrentDownloads int
	RateLimitPerMinute     int
	MaxFileSizeMB          int
	MaxDuration            time.Duration
	TempFileMaxAge         time.Duration
	CleanupInterval        time.Duration
	AllowedOrigin          string
}

func Load() (Config, error) {
	cfg := Config{
		Addr:                   getenv("ADDR", ":8080"),
		YTDLPBin:               getenv("YTDLP_BIN", "yt-dlp"),
		TempDir:                getenv("TEMP_DIR", filepath.Join(os.TempDir(), "yt-download-api")),
		DownloadTimeout:        getDuration("DOWNLOAD_TIMEOUT", 20*time.Minute),
		MaxConcurrentDownloads: getInt("MAX_CONCURRENT_DOWNLOADS", 2),
		RateLimitPerMinute:     getInt("RATE_LIMIT_PER_MINUTE", 10),
		MaxFileSizeMB:          getInt("MAX_FILE_SIZE_MB", 1024),
		MaxDuration:            getDuration("MAX_VIDEO_DURATION", 2*time.Hour),
		TempFileMaxAge:         getDuration("TEMP_FILE_MAX_AGE", 2*time.Hour),
		CleanupInterval:        getDuration("CLEANUP_INTERVAL", 15*time.Minute),
		AllowedOrigin:          getenv("ALLOWED_ORIGIN", "*"),
	}

	if cfg.MaxConcurrentDownloads < 1 {
		return Config{}, errors.New("MAX_CONCURRENT_DOWNLOADS must be >= 1")
	}
	if cfg.RateLimitPerMinute < 1 {
		return Config{}, errors.New("RATE_LIMIT_PER_MINUTE must be >= 1")
	}
	if cfg.MaxFileSizeMB < 1 {
		return Config{}, errors.New("MAX_FILE_SIZE_MB must be >= 1")
	}

	if _, err := exec.LookPath(cfg.YTDLPBin); err != nil {
		return Config{}, fmt.Errorf("yt-dlp binary %q not found in PATH", cfg.YTDLPBin)
	}

	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create temp dir: %w", err)
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
