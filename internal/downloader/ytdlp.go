package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imputnet/cobalt/go-mvp/internal/config"
	"github.com/imputnet/cobalt/go-mvp/internal/jobs"
)

type Runner struct {
	bin             string
	resolveTimeout  time.Duration
	downloadTimeout time.Duration
	sem             chan struct{}
}

type Metadata struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Ext          string `json:"ext"`
	Extractor    string `json:"extractor"`
	ExtractorKey string `json:"extractor_key"`
}

func NewRunner(cfg config.Config) *Runner {
	return &Runner{
		bin:             cfg.YTDLPBin,
		resolveTimeout:  cfg.ResolveTimeout,
		downloadTimeout: cfg.DownloadTimeout,
		sem:             make(chan struct{}, cfg.MaxConcurrentDownloads),
	}
}

func (r *Runner) Resolve(ctx context.Context, rawURL string) (*Metadata, error) {
	ctx, cancel := context.WithTimeout(ctx, r.resolveTimeout)
	defer cancel()

	args := []string{
		"--dump-single-json",
		"--no-warnings",
		"--no-playlist",
		"--skip-download",
		"--",
		rawURL,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp resolve failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var meta Metadata
	if err := json.Unmarshal(stdout.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("parse yt-dlp metadata: %w", err)
	}

	if meta.Title == "" {
		return nil, errors.New("yt-dlp returned empty title")
	}

	return &meta, nil
}

func (r *Runner) Download(ctx context.Context, job *jobs.Job) (string, int64, error) {
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
	defer func() { <-r.sem }()

	ctx, cancel := context.WithTimeout(ctx, r.downloadTimeout)
	defer cancel()

	if err := os.MkdirAll(job.WorkDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create work dir: %w", err)
	}

	args := []string{
		"--no-progress",
		"--no-warnings",
		"--no-playlist",
		"--restrict-filenames",
		"-P", job.WorkDir,
		"-o", "%(title).200B [%(id)s].%(ext)s",
		"--print", "after_move:filepath",
	}

	if job.Mode == "audio" {
		args = append(args, "-x", "--audio-format", job.Format)
		if job.Quality != "" && job.Quality != "best" {
			args = append(args, "--audio-quality", job.Quality+"K")
		}
	} else {
		if shouldUseDefaultSocialVideo(job.Service) {
			args = append(args, "-f", "best")
			args = append(args, "--recode-video", "mp4")
		} else {
			args = append(args, "-f", videoFormatSelector(job.Quality))
			if job.Format != "" {
				args = append(args, "--merge-output-format", job.Format)
			}
		}
	}

	args = append(args, "--", job.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if shouldRetryFormatUnavailable(job, stderr.String()) {
			return r.downloadWithFallback(ctx, job)
		}

		return "", 0, fmt.Errorf("yt-dlp download failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	finalPath := lastNonEmptyLine(stdout.String())
	if finalPath == "" {
		var err error
		finalPath, err = findArtifact(job.WorkDir)
		if err != nil {
			return "", 0, err
		}
	}

	if !filepath.IsAbs(finalPath) {
		finalPath = filepath.Join(job.WorkDir, finalPath)
	}

	info, err := os.Stat(finalPath)
	if err != nil {
		return "", 0, fmt.Errorf("stat final artifact: %w", err)
	}

	if info.IsDir() {
		return "", 0, errors.New("yt-dlp produced a directory instead of a file")
	}

	return finalPath, info.Size(), nil
}

func (r *Runner) downloadWithFallback(ctx context.Context, job *jobs.Job) (string, int64, error) {
	args := []string{
		"--no-progress",
		"--no-warnings",
		"--no-playlist",
		"--restrict-filenames",
		"-P", job.WorkDir,
		"-o", "%(title).200B [%(id)s].%(ext)s",
		"--print", "after_move:filepath",
	}

	if job.Mode == "audio" {
		args = append(args, "-x", "--audio-format", "mp3")
	} else {
		if shouldUseDefaultSocialVideo(job.Service) {
			args = append(args, "-f", "best")
			args = append(args, "--recode-video", "mp4")
		} else {
			args = append(args, "-f", fallbackVideoSelector(job.Quality))
		}
	}

	args = append(args, "--", job.URL)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("yt-dlp download failed after fallback: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	finalPath := lastNonEmptyLine(stdout.String())
	if finalPath == "" {
		var err error
		finalPath, err = findArtifact(job.WorkDir)
		if err != nil {
			return "", 0, err
		}
	}

	if !filepath.IsAbs(finalPath) {
		finalPath = filepath.Join(job.WorkDir, finalPath)
	}

	info, err := os.Stat(finalPath)
	if err != nil {
		return "", 0, fmt.Errorf("stat final artifact: %w", err)
	}

	if info.IsDir() {
		return "", 0, errors.New("yt-dlp produced a directory instead of a file")
	}

	return finalPath, info.Size(), nil
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
		return "", errors.New("no downloaded artifact found")
	}

	return bestPath, nil
}

func videoFormatSelector(quality string) string {
	if quality == "" || quality == "best" {
		return "bv*+ba/b"
	}

	return fmt.Sprintf("bestvideo*[height<=?%s]+bestaudio/best[height<=?%s]/best", quality, quality)
}

func fallbackVideoSelector(quality string) string {
	if quality == "" || quality == "best" {
		return "best"
	}

	return fmt.Sprintf("best[height<=?%s]/best", quality)
}

func shouldRetryFormatUnavailable(job *jobs.Job, stderr string) bool {
	if !strings.Contains(stderr, "Requested format is not available") {
		return false
	}

	return job.Mode == "video"
}

func shouldUseDefaultSocialVideo(service string) bool {
	switch strings.ToLower(service) {
	case "instagram", "tiktok", "pinterest":
		return true
	default:
		return false
	}
}
