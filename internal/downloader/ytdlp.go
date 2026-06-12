package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go-mvp/internal/config"
)

type Runner struct {
	bin             string
	tempDir         string
	downloadTimeout time.Duration
	maxFileSizeMB   int
	maxDuration     time.Duration
	sem             chan struct{}
}

func NewRunner(cfg config.Config) *Runner {
	return &Runner{
		bin:             cfg.YTDLPBin,
		tempDir:         cfg.TempDir,
		downloadTimeout: cfg.DownloadTimeout,
		maxFileSizeMB:   cfg.MaxFileSizeMB,
		maxDuration:     cfg.MaxDuration,
		sem:             make(chan struct{}, cfg.MaxConcurrentDownloads),
	}
}

func (r *Runner) Download(ctx context.Context, rawURL, format, quality string) (string, error) {
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-r.sem }()

	ctx, cancel := context.WithTimeout(ctx, r.downloadTimeout)
	defer cancel()

	workDir, err := os.MkdirTemp(r.tempDir, "download-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	args := r.baseArgs(workDir)
	if format == "mp3" {
		args = append(args, "-x", "--audio-format", "mp3")
	} else {
		args = append(args, "-f", videoSelector(quality), "--merge-output-format", "mp4")
	}
	args = append(args, "--", rawURL)

	started := time.Now()
	log.Printf("yt-dlp started: format=%s quality=%s url=%s work_dir=%s", format, quality, rawURL, workDir)
	path, stderr, err := runYTDLP(ctx, r.bin, args, workDir)
	if err != nil {
		if format == "mp4" && strings.Contains(stderr, "Requested format is not available") {
			return r.downloadBest(ctx, workDir, rawURL)
		}
		_ = os.RemoveAll(workDir)
		return "", fmt.Errorf("yt-dlp failed: %w: %s", err, strings.TrimSpace(stderr))
	}

	logDownloadStats("yt-dlp finished", path, time.Since(started))
	return path, nil
}

func (r *Runner) downloadBest(ctx context.Context, workDir, rawURL string) (string, error) {
	args := append(r.baseArgs(workDir), "-f", "best", "--merge-output-format", "mp4", "--", rawURL)
	started := time.Now()
	log.Printf("yt-dlp fallback started: url=%s work_dir=%s", rawURL, workDir)
	path, stderr, err := runYTDLP(ctx, r.bin, args, workDir)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", fmt.Errorf("yt-dlp failed: %w: %s", err, strings.TrimSpace(stderr))
	}

	logDownloadStats("yt-dlp fallback finished", path, time.Since(started))
	return path, nil
}

func (r *Runner) baseArgs(workDir string) []string {
	args := []string{
		"--newline",
		"--no-warnings",
		"--no-playlist",
		"--restrict-filenames",
		"--socket-timeout", "15",
		"--retries", "2",
		"--fragment-retries", "2",
		"--concurrent-fragments", "4",
		"-P", workDir,
		"-o", "%(title).200B [%(id)s].%(ext)s",
		"--print", "after_move:filepath",
	}
	if r.maxFileSizeMB > 0 {
		args = append(args, "--max-filesize", fmt.Sprintf("%dM", r.maxFileSizeMB))
	}
	if r.maxDuration > 0 {
		args = append(args, "--match-filter", fmt.Sprintf("duration <= %d", int(r.maxDuration.Seconds())))
	}
	return args
}

func runYTDLP(ctx context.Context, bin string, args []string, workDir string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Run(); err != nil {
		return "", stderr.String(), err
	}

	path := lastNonEmptyLine(stdout.String())
	if path == "" {
		var err error
		path, err = findArtifact(workDir)
		if err != nil {
			return "", stderr.String(), err
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}

	return path, stderr.String(), nil
}

func logDownloadStats(prefix, path string, duration time.Duration) {
	info, err := os.Stat(path)
	if err != nil {
		log.Printf("%s: path=%s duration=%s stat_error=%v", prefix, path, duration.Round(time.Millisecond), err)
		return
	}

	sizeMB := float64(info.Size()) / 1024 / 1024
	speedMBps := 0.0
	if duration > 0 {
		speedMBps = sizeMB / duration.Seconds()
	}
	log.Printf("%s: path=%s size=%.1fMiB duration=%s avg_speed=%.2fMiB/s", prefix, path, sizeMB, duration.Round(time.Millisecond), speedMBps)
}

func CleanupTempDir(root string, maxAge time.Duration) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "download-") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			log.Printf("temp cleanup stat failed: path=%s error=%v", path, err)
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(path); err != nil {
				log.Printf("temp cleanup remove failed: path=%s error=%v", path, err)
			} else {
				log.Printf("temp cleanup removed stale dir: path=%s age=%s", path, time.Since(info.ModTime()).Round(time.Second))
			}
		}
	}
	return nil
}

func StartTempCleaner(ctx context.Context, root string, maxAge, interval time.Duration) {
	if interval <= 0 || maxAge <= 0 {
		return
	}
	go func() {
		if err := CleanupTempDir(root, maxAge); err != nil {
			log.Printf("temp cleanup failed: error=%v", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := CleanupTempDir(root, maxAge); err != nil {
					log.Printf("temp cleanup failed: error=%v", err)
				}
			}
		}
	}()
}

func videoSelector(quality string) string {
	if quality == "" {
		quality = "1080"
	}
	return fmt.Sprintf("bestvideo*[height<=?%s]+bestaudio/best[height<=?%s]/best", quality, quality)
}

func lastNonEmptyLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func findArtifact(root string) (string, error) {
	var bestPath string
	var bestSize int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() >= bestSize {
			bestPath = path
			bestSize = info.Size()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if bestPath == "" {
		return "", errors.New("no downloaded file found")
	}
	return bestPath, nil
}
