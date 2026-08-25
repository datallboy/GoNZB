package uploader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type InboxLogger interface {
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
}

type InboxOptions struct {
	Root         string
	ScanInterval time.Duration
	SettleAge    time.Duration
	MaxNZBBytes  int64
	Logger       InboxLogger
}

type InboxScanner struct {
	service *Service
	opts    InboxOptions
	now     func() time.Time
}

type inboxFailureStore interface {
	ShouldAttemptInboxPath(context.Context, string, time.Time, int64, time.Time) (bool, error)
	RecordInboxFailure(context.Context, string, time.Time, int64, string, string, time.Time) error
	ClearInboxFailure(context.Context, string) error
}

type ScanResult struct {
	Eligible  int `json:"eligible"`
	Created   int `json:"created"`
	Duplicate int `json:"duplicate"`
	Failed    int `json:"failed"`
}

func NewInboxScanner(service *Service, opts InboxOptions) *InboxScanner {
	if opts.ScanInterval <= 0 {
		opts.ScanInterval = 15 * time.Second
	}
	if opts.SettleAge < 0 {
		opts.SettleAge = 0
	}
	if opts.MaxNZBBytes <= 0 {
		opts.MaxNZBBytes = 64 << 20
	}
	return &InboxScanner{service: service, opts: opts, now: time.Now}
}

func (s *InboxScanner) Start(ctx context.Context) error {
	if err := s.validateRoot(); err != nil {
		return err
	}
	if _, err := s.ScanOnce(ctx); err != nil && s.opts.Logger != nil {
		s.opts.Logger.Warn("uploader inbox initial scan failed: %v", err)
	}
	ticker := time.NewTicker(s.opts.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			result, err := s.ScanOnce(ctx)
			if err != nil {
				if s.opts.Logger != nil {
					s.opts.Logger.Warn("uploader inbox scan failed: %v", err)
				}
				continue
			}
			if s.opts.Logger != nil && (result.Created > 0 || result.Failed > 0) {
				s.opts.Logger.Info("uploader inbox scan eligible=%d created=%d duplicate=%d failed=%d", result.Eligible, result.Created, result.Duplicate, result.Failed)
			}
		}
	}
}

func (s *InboxScanner) ScanOnce(ctx context.Context) (ScanResult, error) {
	var result ScanResult
	root, err := s.canonicalRoot()
	if err != nil {
		return result, err
	}
	cutoff := s.now().UTC().Add(-s.opts.SettleAge)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Failed++
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".nzb") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			result.Failed++
			return nil
		}
		if info.ModTime().UTC().After(cutoff) {
			return nil
		}
		fingerprint := inboxPathFingerprint(safeRelative(root, path))
		if failures, ok := s.service.store.(inboxFailureStore); ok {
			attempt, err := failures.ShouldAttemptInboxPath(ctx, fingerprint, info.ModTime(), info.Size(), s.now())
			if err != nil {
				result.Failed++
				return nil
			}
			if !attempt {
				return nil
			}
		}
		if info.Size() <= 0 || info.Size() > s.opts.MaxNZBBytes {
			result.Eligible++
			result.Failed++
			s.recordFailure(ctx, fingerprint, info, "size_invalid", "NZB size is outside configured limits")
			return nil
		}
		if !pathWithin(root, path) {
			result.Failed++
			return nil
		}
		result.Eligible++
		payload, err := readStableRegularFile(path, info, s.opts.MaxNZBBytes)
		if err != nil {
			result.Failed++
			s.recordFailure(ctx, fingerprint, info, "read_failed", "NZB could not be read as a stable regular file")
			return nil
		}
		created, err := s.service.Submit(ctx, SubmitInput{
			NZBBytes:         payload,
			OriginalFilename: entry.Name(),
			IntakeKind:       IntakeInbox,
			SubmittedBy:      "system:inbox",
		})
		if err != nil {
			result.Failed++
			s.recordFailure(ctx, fingerprint, info, "validation_failed", err.Error())
			if s.opts.Logger != nil {
				s.opts.Logger.Warn("uploader inbox rejected path=%s error=%v", safeRelative(root, path), err)
			}
			return nil
		}
		if failures, ok := s.service.store.(inboxFailureStore); ok {
			_ = failures.ClearInboxFailure(ctx, fingerprint)
		}
		if created.Created {
			result.Created++
		} else {
			result.Duplicate++
		}
		return nil
	})
	return result, err
}

func (s *InboxScanner) recordFailure(ctx context.Context, fingerprint string, info os.FileInfo, code, message string) {
	if failures, ok := s.service.store.(inboxFailureStore); ok {
		_ = failures.RecordInboxFailure(ctx, fingerprint, info.ModTime(), info.Size(), code, message, s.now().UTC().Add(5*time.Minute))
	}
}

func inboxPathFingerprint(relativePath string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(strings.TrimSpace(relativePath))))
	return hex.EncodeToString(sum[:])
}

func (s *InboxScanner) validateRoot() error {
	_, err := s.canonicalRoot()
	return err
}

func (s *InboxScanner) Check() error {
	return s.validateRoot()
}

func (s *InboxScanner) canonicalRoot() (string, error) {
	root := strings.TrimSpace(s.opts.Root)
	if root == "" {
		return "", fmt.Errorf("uploader inbox root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("inspect uploader inbox root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("uploader inbox root must be a real directory")
	}
	return filepath.Clean(abs), nil
}

func readStableRegularFile(path string, before os.FileInfo, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("nzb exceeds byte limit")
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, fmt.Errorf("nzb changed while being read")
	}
	return payload, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func safeRelative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "<outside-inbox>"
	}
	return filepath.ToSlash(rel)
}
