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
	JobTTL                 time.Duration
	ResolveTimeout         time.Duration
	DownloadTimeout        time.Duration
	MaxConcurrentDownloads int
}

func Load() (Config, error) {
	cfg := Config{
		Addr:                   getenv("ADDR", ":8080"),
		YTDLPBin:               getenv("YTDLP_BIN", "yt-dlp"),
		TempDir:                getenv("TEMP_DIR", filepath.Join(os.TempDir(), "cobalt-go-mvp")),
		JobTTL:                 getDuration("JOB_TTL", 30*time.Minute),
		ResolveTimeout:         getDuration("RESOLVE_TIMEOUT", 20*time.Second),
		DownloadTimeout:        getDuration("DOWNLOAD_TIMEOUT", 20*time.Minute),
		MaxConcurrentDownloads: getInt("MAX_CONCURRENT_DOWNLOADS", 2),
	}

	if cfg.MaxConcurrentDownloads < 1 {
		return Config{}, errors.New("MAX_CONCURRENT_DOWNLOADS must be >= 1")
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
