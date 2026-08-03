package resolver

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
)

type sourceResolver interface {
	GetRelease(ctx context.Context, sourceReleaseID string) (*domain.Release, error)
	GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error)
}

type DefaultReleaseResolver struct {
	aggregator  sourceResolver
	usenetIndex sourceResolver
}

func NewDefaultReleaseResolver(
	aggregator sourceResolver,
	usenetIndex sourceResolver,
) *DefaultReleaseResolver {
	return &DefaultReleaseResolver{
		aggregator:  aggregator,
		usenetIndex: usenetIndex,
	}
}

// route release lookup using persisted queue provenance.
func (r *DefaultReleaseResolver) GetRelease(ctx context.Context, sourceKind, sourceReleaseID string) (*domain.Release, error) {
	resolver, err := r.pickResolver(sourceKind)
	if err != nil {
		return nil, err
	}
	return resolver.GetRelease(ctx, sourceReleaseID)
}

// route payload fetch using source kind, not release.Source heuristics.
func (r *DefaultReleaseResolver) GetNZB(ctx context.Context, sourceKind string, rel *domain.Release) (io.ReadCloser, error) {
	resolver, err := r.pickResolver(sourceKind)
	if err != nil {
		return nil, err
	}
	return resolver.GetNZB(ctx, rel)
}

func (r *DefaultReleaseResolver) pickResolver(sourceKind string) (sourceResolver, error) {
	switch strings.TrimSpace(strings.ToLower(sourceKind)) {
	case "aggregator":
		if r.aggregator == nil {
			return nil, fmt.Errorf("aggregator resolver is not configured")
		}
		return r.aggregator, nil
	case "usenet_index":
		if r.usenetIndex == nil {
			return nil, fmt.Errorf("usenet_index resolver is not configured")
		}
		return r.usenetIndex, nil
	default:
		return nil, fmt.Errorf("unsupported source kind %q", sourceKind)
	}
}
