package jobs

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Job struct {
	ID        string
	URL       string
	Mode      string
	Quality   string
	Format    string
	Service   string
	Title     string
	Filename  string
	CreatedAt time.Time
	ExpiresAt time.Time
	WorkDir   string

	mu           sync.Mutex
	cond         *sync.Cond
	artifactPath string
	artifactSize int64
	building     bool
}

func newJob(id, url, mode, quality, format, service, title, filename, workDir string, ttl time.Duration) *Job {
	job := &Job{
		ID:        id,
		URL:       url,
		Mode:      mode,
		Quality:   quality,
		Format:    format,
		Service:   service,
		Title:     title,
		Filename:  filename,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(ttl),
		WorkDir:   workDir,
	}
	job.cond = sync.NewCond(&job.mu)
	return job
}

func (j *Job) EnsureArtifact(ctx context.Context, build func(context.Context) (string, int64, error)) (string, int64, error) {
	j.mu.Lock()
	for j.building {
		j.cond.Wait()
	}

	if j.artifactPath != "" {
		path := j.artifactPath
		size := j.artifactSize
		j.mu.Unlock()
		return path, size, nil
	}

	j.building = true
	j.mu.Unlock()

	path, size, err := build(ctx)

	j.mu.Lock()
	defer j.mu.Unlock()

	if err == nil {
		j.artifactPath = path
		j.artifactSize = size
	}

	j.building = false
	j.cond.Broadcast()

	return path, size, err
}

func (j *Job) Cleanup() {
	_ = os.RemoveAll(j.WorkDir)
}

type Store struct {
	mu      sync.RWMutex
	jobs    map[string]*Job
	ttl     time.Duration
	tempDir string
}

func NewStore(ttl time.Duration, tempDir string) *Store {
	return &Store{
		jobs:    make(map[string]*Job),
		ttl:     ttl,
		tempDir: tempDir,
	}
}

func (s *Store) Create(url, mode, quality, format, service, title, filename string) *Job {
	id := randomID()
	job := newJob(id, url, mode, quality, format, service, title, filename, filepath.Join(s.tempDir, id), s.ttl)

	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()

	return job
}

func (s *Store) Get(id string) (*Job, bool) {
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}

	if time.Now().UTC().After(job.ExpiresAt) {
		s.Delete(id)
		return nil, false
	}

	return job, true
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if ok {
		delete(s.jobs, id)
	}
	s.mu.Unlock()

	if ok {
		job.Cleanup()
	}
}

func (s *Store) StartJanitor(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.cleanupAll()
			return
		case <-ticker.C:
			s.cleanupExpired()
		}
	}
}

func (s *Store) cleanupExpired() {
	now := time.Now().UTC()
	var expired []string

	s.mu.RLock()
	for id, job := range s.jobs {
		if now.After(job.ExpiresAt) {
			expired = append(expired, id)
		}
	}
	s.mu.RUnlock()

	for _, id := range expired {
		s.Delete(id)
	}
}

func (s *Store) cleanupAll() {
	s.mu.Lock()
	jobs := s.jobs
	s.jobs = make(map[string]*Job)
	s.mu.Unlock()

	for _, job := range jobs {
		job.Cleanup()
	}
}

func randomID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
