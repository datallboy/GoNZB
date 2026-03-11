package resolver

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

type usenetIndexCatalog interface {
	GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error)
}

type usenetIndexResolver struct {
	catalog usenetIndexCatalog
}

func NewUsenetIndexResolver(catalog usenetIndexCatalog) *usenetIndexResolver {
	return &usenetIndexResolver{catalog: catalog}
}

func (r *usenetIndexResolver) GetRelease(ctx context.Context, sourceReleaseID string) (*domain.Release, error) {
	if r.catalog == nil {
		return nil, fmt.Errorf("usenet index catalog is not configured")
	}

	sourceReleaseID = strings.TrimSpace(sourceReleaseID)
	if sourceReleaseID == "" {
		return nil, fmt.Errorf("source release id is required")
	}

	return r.catalog.GetCatalogReleaseByID(ctx, sourceReleaseID)
}

func (r *usenetIndexResolver) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	_ = ctx
	_ = rel

	// metadata-only in chunk 1. NZB materialization comes in Milestone 7 chunk 4.
	return nil, fmt.Errorf("usenet_index NZB resolution is not implemented yet")
}
