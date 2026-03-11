package resolver

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

type manualPayloadStore interface {
	GetNZBReader(key string) (io.ReadCloser, error)
}

type manualResolver struct {
	store manualPayloadStore
}

func NewManualResolver(store manualPayloadStore) *manualResolver {
	return &manualResolver{store: store}
}

func (r *manualResolver) GetRelease(ctx context.Context, sourceReleaseID string) (*domain.Release, error) {
	_ = ctx

	sourceReleaseID = strings.TrimSpace(sourceReleaseID)
	if sourceReleaseID == "" {
		return nil, fmt.Errorf("source release id is required")
	}

	// manual mode reconstructs minimal release metadata without aggregator/PG.
	return &domain.Release{
		ID:          sourceReleaseID,
		GUID:        sourceReleaseID,
		Title:       sourceReleaseID,
		Source:      "manual",
		Category:    "Uncategorized",
		PublishDate: time.Time{},
	}, nil
}

func (r *manualResolver) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	_ = ctx

	if r.store == nil {
		return nil, fmt.Errorf("manual payload store is not configured")
	}
	if rel == nil || strings.TrimSpace(rel.ID) == "" {
		return nil, fmt.Errorf("manual release is required")
	}

	return r.store.GetNZBReader(rel.ID)
}
