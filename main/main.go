package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-mvp/internal/config"
	"go-mvp/internal/downloader"
	"go-mvp/internal/httpapi"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.LUTC)

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	downloader.StartTempCleaner(ctx, cfg.TempDir, cfg.TempFileMaxAge, cfg.CleanupInterval)

	runner := downloader.NewRunner(cfg)
	api := httpapi.New(runner)

	mux := http.NewServeMux()
	api.Register(mux)

	handler := httpapi.NewRateLimiter(cfg.RateLimitPerMinute, time.Minute)(mux)
	handler = httpapi.LogRequests(handler)
	handler = httpapi.WithCORS(handler, cfg.AllowedOrigin)
	handler = httpapi.WithSecurityHeaders(handler)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      cfg.DownloadTimeout + 5*time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		log.Printf("shutting down server")
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown failed: %v", err)
		}
	}()

	log.Printf("starting youtube-download-api addr=%s temp_dir=%s ytdlp_bin=%s max_concurrent_downloads=%d rate_limit_per_minute=%d max_file_size_mb=%d max_duration=%s download_timeout=%s", cfg.Addr, cfg.TempDir, cfg.YTDLPBin, cfg.MaxConcurrentDownloads, cfg.RateLimitPerMinute, cfg.MaxFileSizeMB, cfg.MaxDuration, cfg.DownloadTimeout)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
