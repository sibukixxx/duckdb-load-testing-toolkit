package e2e

import (
	"context"
	"sync"
)

// UploadRecord captures a single upload call made to StubUploader.
type UploadRecord struct {
	Key      string
	FilePath string
}

// StubUploader is a thread-safe in-memory stub for handlers.Uploader.
// It records upload calls so tests can assert on them.
type StubUploader struct {
	mu      sync.Mutex
	uploads []UploadRecord
	enabled bool
}

// NewStubUploader returns a configured StubUploader.
func NewStubUploader() *StubUploader {
	return &StubUploader{enabled: true}
}

// DisabledStubUploader returns a stub that reports IsConfigured() == false.
func DisabledStubUploader() *StubUploader {
	return &StubUploader{enabled: false}
}

func (s *StubUploader) IsConfigured() bool {
	return s.enabled
}

func (s *StubUploader) UploadFile(_ context.Context, key, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads = append(s.uploads, UploadRecord{Key: key, FilePath: filePath})
	return nil
}

// Uploads returns a snapshot of all upload calls received so far.
func (s *StubUploader) Uploads() []UploadRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]UploadRecord, len(s.uploads))
	copy(result, s.uploads)
	return result
}

// ClearUploads resets the recorded uploads.
func (s *StubUploader) ClearUploads() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploads = nil
}
