package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-mvp/internal/downloader"
	"go-mvp/internal/jobs"
)

type Server struct {
	store  *jobs.Store
	runner *downloader.Runner
}

type resolveRequest struct {
	URL     string `json:"url"`
	Mode    string `json:"mode"`
	Quality string `json:"quality"`
	Format  string `json:"format"`
}

type resolveResponse struct {
	ID          string    `json:"id"`
	Service     string    `json:"service"`
	Title       string    `json:"title"`
	Filename    string    `json:"filename"`
	Mode        string    `json:"mode"`
	Quality     string    `json:"quality"`
	Format      string    `json:"format"`
	DownloadURL string    `json:"downloadUrl"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func New(store *jobs.Store, runner *downloader.Runner) *Server {
	return &Server{
		store:  store,
		runner: runner,
	}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/resolve", s.handleResolve)
	mux.HandleFunc("/download/", s.handleDownload)
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":      "cobalt-go-mvp",
		"endpoints": []string{"/healthz", "/resolve", "/download/{id}"},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	req.Quality = strings.ToLower(strings.TrimSpace(req.Quality))
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Mode == "" {
		req.Mode = "video"
	}

	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "missing_url")
		return
	}

	if req.Mode != "video" && req.Mode != "audio" {
		writeError(w, http.StatusBadRequest, "invalid_mode")
		return
	}

	if req.Quality == "" {
		req.Quality = "best"
	}

	if req.Mode == "audio" && req.Format == "" {
		req.Format = "mp3"
	}

	if !validQuality(req.Mode, req.Quality) {
		writeError(w, http.StatusBadRequest, "invalid_quality")
		return
	}

	if !validFormat(req.Mode, req.Format) {
		writeError(w, http.StatusBadRequest, "invalid_format")
		return
	}

	service, ok := downloader.SupportedService(req.URL)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported_service")
		return
	}

	meta, err := s.runner.Resolve(r.Context(), req.URL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	if resolvedService, ok := downloader.ServiceFromExtractor(meta.Extractor); ok {
		service = resolvedService
	}

	if req.Mode == "video" && shouldForceDefaultSocialVideo(service) {
		req.Quality = "best"
		req.Format = "mp4"
	}

	filename := buildFilename(meta.Title, req.Mode, req.Format, meta.Ext)
	job := s.store.Create(req.URL, req.Mode, req.Quality, req.Format, service, meta.Title, filename)
	responseFormat := req.Format
	if responseFormat == "" {
		responseFormat = strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	}

	writeJSON(w, http.StatusOK, resolveResponse{
		ID:          job.ID,
		Service:     service,
		Title:       meta.Title,
		Filename:    filename,
		Mode:        req.Mode,
		Quality:     req.Quality,
		Format:      responseFormat,
		DownloadURL: absoluteDownloadURL(r, job.ID),
		ExpiresAt:   job.ExpiresAt,
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/download/")
	id, _ = url.PathUnescape(id)
	if id == "" {
		writeError(w, http.StatusNotFound, "job_not_found")
		return
	}

	job, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "job_not_found")
		return
	}

	path, _, err := job.EnsureArtifact(r.Context(), func(ctx context.Context) (string, int64, error) {
		return s.runner.Download(ctx, job)
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	file, err := filepath.Abs(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "artifact_path_error")
		return
	}

	fh, err := os.Open(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "open_artifact_failed")
		return
	}
	defer fh.Close()

	stat, err := fh.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stat_artifact_failed")
		return
	}

	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(job.Filename)))
	if contentType == "" {
		var sniff [512]byte
		n, _ := io.ReadFull(fh, sniff[:])
		_, _ = fh.Seek(0, io.SeekStart)
		contentType = http.DetectContentType(sniff[:n])
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(job.Filename))

	http.ServeContent(w, r, job.Filename, stat.ModTime(), fh)
}

func buildFilename(title, mode, format, ext string) string {
	name := sanitizeFilename(title)
	if name == "" {
		name = "download"
	}

	if mode == "audio" {
		if format == "" {
			format = "mp3"
		}
		return name + "." + format
	}

	if format != "" {
		return name + "." + format
	}

	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if ext == "" {
		ext = "mp4"
	}

	return name + "." + ext
}

func sanitizeFilename(input string) string {
	var b strings.Builder

	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		}
	}

	return strings.TrimSpace(b.String())
}

func contentDisposition(filename string) string {
	return fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename))
}

func validQuality(mode, quality string) bool {
	switch mode {
	case "video":
		switch quality {
		case "best", "4320", "2160", "1440", "1080", "720", "480", "360", "240", "144":
			return true
		}
	case "audio":
		switch quality {
		case "best", "320", "256", "192", "128", "96", "64":
			return true
		}
	}

	return false
}

func validFormat(mode, format string) bool {
	if format == "" {
		return true
	}

	switch mode {
	case "video":
		switch format {
		case "mp4", "webm", "mkv":
			return true
		}
	case "audio":
		switch format {
		case "mp3", "m4a", "opus", "wav", "flac":
			return true
		}
	}

	return false
}

func shouldForceDefaultSocialVideo(service string) bool {
	switch strings.ToLower(service) {
	case "instagram", "tiktok", "pinterest":
		return true
	default:
		return false
	}
}

func absoluteDownloadURL(r *http.Request, jobID string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = strings.Split(forwardedProto, ",")[0]
	}

	host := strings.TrimSpace(r.Host)
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}

	return (&url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   "/download/" + url.PathEscape(jobID),
	}).String()
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"status": "error",
		"error":  message,
	})
}
