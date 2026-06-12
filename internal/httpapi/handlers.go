package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-mvp/internal/downloader"
)

type Server struct {
	runner *downloader.Runner
}

func New(runner *downloader.Runner) *Server {
	return &Server{runner: runner}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/download", s.handleDownload)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

// LogRequests logs every API request with method, path, status, response bytes, duration, and client IP.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w}

		log.Printf("api request started method=%s path=%s query=%q remote=%s user_agent=%q", r.Method, r.URL.Path, r.URL.RawQuery, clientIP(r), r.UserAgent())
		next.ServeHTTP(lrw, r)

		status := lrw.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("api request completed method=%s path=%s status=%d bytes=%d duration=%s remote=%s", r.Method, r.URL.Path, status, lrw.bytes, time.Since(start).Round(time.Millisecond), clientIP(r))
	})
}

func WithSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func WithCORS(next http.Handler, allowedOrigin string) http.Handler {
	if allowedOrigin == "" {
		allowedOrigin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]*rateState
}

type rateState struct {
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

func NewRateLimiter(limit int, window time.Duration) func(http.Handler) http.Handler {
	if limit < 1 {
		limit = 1
	}
	limiter := &rateLimiter{limit: limit, window: window, clients: make(map[string]*rateState)}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions || r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.allow(clientIP(r)) {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", window.Seconds()))
				writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *rateLimiter) allow(client string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, state := range l.clients {
		if now.Sub(state.lastSeen) > 2*l.window {
			delete(l.clients, key)
		}
	}

	state := l.clients[client]
	if state == nil || now.Sub(state.windowStart) >= l.window {
		l.clients[client] = &rateState{count: 1, windowStart: now, lastSeen: now}
		return true
	}
	state.lastSeen = now
	if state.count >= l.limit {
		return false
	}
	state.count++
	return true
}

func clientIP(r *http.Request) string {
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		return strings.TrimSpace(strings.Split(forwardedFor, ",")[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	return r.RemoteAddr
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":  "youtube-download-api",
		"usage": "/download?url=YOUTUBE_URL&format=mp4&quality=1080",
		"query": map[string]string{
			"url":     "required YouTube URL",
			"format":  "optional: mp4 or mp3, default mp4",
			"quality": "optional for mp4: 144-4320, default 1080",
		},
		"endpoints": map[string]string{
			"health":   "/healthz",
			"download": "/download?url=YOUTUBE_URL&format=mp4&quality=1080",
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	q := r.URL.Query()
	rawURL := strings.TrimSpace(q.Get("url"))
	format := strings.ToLower(strings.TrimSpace(q.Get("format")))
	quality := strings.TrimSpace(q.Get("quality"))

	if rawURL == "" {
		log.Printf("download rejected: missing url remote=%s", clientIP(r))
		writeError(w, http.StatusBadRequest, "missing_url")
		return
	}
	if !downloader.IsYouTube(rawURL) {
		log.Printf("download rejected: unsupported url=%q remote=%s", rawURL, clientIP(r))
		writeError(w, http.StatusBadRequest, "only_youtube_urls_are_supported")
		return
	}
	if format == "" {
		format = "mp4"
	}
	if format != "mp4" && format != "mp3" {
		log.Printf("download rejected: invalid format=%q url=%q remote=%s", format, rawURL, clientIP(r))
		writeError(w, http.StatusBadRequest, "format_must_be_mp4_or_mp3")
		return
	}
	if quality == "" {
		quality = "1080"
	}
	qualityNumber, err := strconv.Atoi(quality)
	if err != nil || qualityNumber < 144 || qualityNumber > 4320 {
		log.Printf("download rejected: invalid quality=%q url=%q remote=%s", quality, rawURL, clientIP(r))
		writeError(w, http.StatusBadRequest, "quality_must_be_between_144_and_4320")
		return
	}

	requestStarted := time.Now()
	downloadStarted := time.Now()
	log.Printf("download started: url=%q format=%s quality=%s", rawURL, format, quality)
	path, err := s.runner.Download(r.Context(), rawURL, format, quality)
	if err != nil {
		log.Printf("download failed: url=%q format=%s quality=%s duration=%s error=%v", rawURL, format, quality, time.Since(downloadStarted).Round(time.Millisecond), err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	log.Printf("download ready: path=%q duration=%s", path, time.Since(downloadStarted).Round(time.Millisecond))
	defer os.RemoveAll(filepath.Dir(path))

	fh, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "open_file_failed")
		return
	}
	defer fh.Close()

	stat, err := fh.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stat_file_failed")
		return
	}

	filename := filepath.Base(path)
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	if contentType == "" {
		var sniff [512]byte
		n, _ := io.ReadFull(fh, sniff[:])
		_, _ = fh.Seek(0, io.SeekStart)
		contentType = http.DetectContentType(sniff[:n])
	}

	serveStarted := time.Now()
	log.Printf("sending file: %s size=%d", filename, stat.Size())
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition(filename))
	http.ServeContent(w, r, filename, stat.ModTime(), fh)
	log.Printf("file sent: %s size=%d send_duration=%s total_request_duration=%s", filename, stat.Size(), time.Since(serveStarted).Round(time.Millisecond), time.Since(requestStarted).Round(time.Millisecond))
}

func contentDisposition(filename string) string {
	return fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"status": "error", "error": message})
}
