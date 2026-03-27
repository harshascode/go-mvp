package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/imputnet/cobalt/go-mvp/internal/config"
	"github.com/imputnet/cobalt/go-mvp/internal/downloader"
	"github.com/imputnet/cobalt/go-mvp/internal/httpapi"
	"github.com/imputnet/cobalt/go-mvp/internal/jobs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	store := jobs.NewStore(cfg.JobTTL, cfg.TempDir)
	runner := downloader.NewRunner(cfg)
	api := httpapi.New(store, runner)

	mux := http.NewServeMux()
	api.Register(mux)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go store.StartJanitor(ctx)

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("go-mvp listening on %s using yt-dlp=%s temp_dir=%s", cfg.Addr, cfg.YTDLPBin, cfg.TempDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
