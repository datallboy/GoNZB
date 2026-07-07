package releasegenerate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type repository interface {
	ListReleaseNZBGenerateCandidates(ctx context.Context, limit int, policy pgindex.ReleaseReadyPolicy) ([]pgindex.ReleaseNZBGenerateCandidate, error)
	MarkReleaseArchiveStored(ctx context.Context, in pgindex.ReleaseArchiveStoredRecord) error
	MarkReleaseArchiveFailed(ctx context.Context, releaseID, errText string) error
}

type nzbResolver interface {
	GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error)
}

type blobStore interface {
	SaveNZBAtomically(key string, data []byte) error
}

type Options struct {
	BatchSize int
	Policy    pgindex.ReleaseReadyPolicy
}

type Service struct {
	repo     repository
	resolver nzbResolver
	store    blobStore
	opts     Options
}

func NewService(repo repository, resolver nzbResolver, store blobStore, opts Options) *Service {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}
	if opts.Policy == (pgindex.ReleaseReadyPolicy{}) {
		opts.Policy = pgindex.DefaultReleaseReadyPolicy()
	} else {
		opts.Policy = pgindex.NormalizeReleaseReadyPolicy(opts.Policy)
	}
	return &Service{repo: repo, resolver: resolver, store: store, opts: opts}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	if s.repo == nil || s.resolver == nil || s.store == nil {
		return nil, fmt.Errorf("generate service dependencies are required")
	}

	candidates, err := s.repo.ListReleaseNZBGenerateCandidates(ctx, s.opts.BatchSize, s.opts.Policy)
	if err != nil {
		return nil, err
	}

	metrics := map[string]any{
		"generate_candidates": len(candidates),
		"generate_attempted":  0,
		"archived_count":      0,
		"generate_failures":   0,
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return metrics, err
		}
		metrics["generate_attempted"] = metrics["generate_attempted"].(int) + 1
		reader, err := s.resolver.GetNZB(ctx, &domain.Release{
			ID:          candidate.ReleaseID,
			Title:       candidate.Title,
			GUID:        candidate.ReleaseID,
			Source:      "usenet_index",
			PublishDate: time.Now().UTC(),
		})
		if err != nil {
			metrics["generate_failures"] = metrics["generate_failures"].(int) + 1
			_ = s.repo.MarkReleaseArchiveFailed(ctx, candidate.ReleaseID, err.Error())
			continue
		}

		payload, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			metrics["generate_failures"] = metrics["generate_failures"].(int) + 1
			_ = s.repo.MarkReleaseArchiveFailed(ctx, candidate.ReleaseID, readErr.Error())
			continue
		}

		hash := sha256.Sum256(payload)
		hashHex := hex.EncodeToString(hash[:])
		objectKey := archiveObjectKey(candidate.ProviderID, candidate.ReleaseID, hashHex)
		if err := s.store.SaveNZBAtomically(objectKey, payload); err != nil {
			metrics["generate_failures"] = metrics["generate_failures"].(int) + 1
			_ = s.repo.MarkReleaseArchiveFailed(ctx, candidate.ReleaseID, err.Error())
			continue
		}
		if err := s.repo.MarkReleaseArchiveStored(ctx, pgindex.ReleaseArchiveStoredRecord{
			ReleaseID:         candidate.ReleaseID,
			ArchiveStore:      "indexer_archive",
			ObjectStoreKind:   "fs",
			ObjectKey:         objectKey,
			ContentHashSHA256: hashHex,
			ObjectSizeBytes:   int64(len(payload)),
			ContentEncoding:   "identity",
			SourceModule:      "usenet_index",
		}); err != nil {
			metrics["generate_failures"] = metrics["generate_failures"].(int) + 1
			_ = s.repo.MarkReleaseArchiveFailed(ctx, candidate.ReleaseID, err.Error())
			continue
		}

		metrics["archived_count"] = metrics["archived_count"].(int) + 1
	}

	return metrics, nil
}

func archiveObjectKey(providerID int64, releaseID, hashHex string) string {
	releaseID = strings.TrimSpace(releaseID)
	hashHex = strings.TrimSpace(hashHex)
	if hashHex == "" {
		hashHex = "unknown"
	}
	return fmt.Sprintf("releases/%d/%s/%s.nzb", providerID, releaseID, hashHex)
}
