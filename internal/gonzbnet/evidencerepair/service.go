package evidencerepair

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/evidenceclient"
	gonzbnetmetrics "github.com/datallboy/gonzb/internal/gonzbnet/metrics"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Store interface {
	RefreshBinaryExchangeIdentities(context.Context, int) (int, error)
	SeedBinaryEvidenceRepairWork(context.Context, int) (int, error)
	ClaimBinaryEvidenceRepairWork(context.Context, string, int, time.Duration) ([]pgindex.BinaryEvidenceRepairCandidate, error)
	CompleteBinaryEvidenceRepairWork(context.Context, int64, bool, string) error
	BinaryEffectiveComplete(context.Context, int64) (bool, error)
}

type SegmentClient interface {
	LookupSegments(context.Context, int64, string, string, []int, []string) (evidenceclient.SegmentLookupResult, error)
}

type Result struct {
	Identities   int
	Seeded       int
	Claimed      int
	Imported     int
	Completed    int
	Conflicts    int
	PeerRequests int
}

type Service struct {
	store     Store
	client    SegmentClient
	owner     string
	batchSize int
}

func New(store Store, client SegmentClient, owner string, batchSize int) *Service {
	if batchSize <= 0 {
		batchSize = 10
	}
	return &Service{store: store, client: client, owner: owner, batchSize: batchSize}
}

func (s *Service) RunOnce(ctx context.Context) (Result, error) {
	var result Result
	if s == nil || s.store == nil || s.client == nil {
		return result, fmt.Errorf("binary evidence repair service is not configured")
	}
	var err error
	result.Identities, err = s.store.RefreshBinaryExchangeIdentities(ctx, 1000)
	if err != nil {
		return result, err
	}
	result.Seeded, err = s.store.SeedBinaryEvidenceRepairWork(ctx, 500)
	if err != nil {
		return result, err
	}
	items, err := s.store.ClaimBinaryEvidenceRepairWork(ctx, s.owner, s.batchSize, 2*time.Minute)
	if err != nil {
		return result, err
	}
	result.Claimed = len(items)
	for _, item := range items {
		if len(item.MissingParts) == 0 {
			if err := s.store.CompleteBinaryEvidenceRepairWork(ctx, item.BinaryID, true, ""); err != nil {
				return result, err
			}
			result.Completed++
			continue
		}
		lookup, lookupErr := s.client.LookupSegments(
			ctx, item.BinaryID, item.Scheme, item.MatchID,
			item.MissingParts, item.Anchors,
		)
		result.Imported += lookup.Imported
		result.Conflicts += lookup.Conflicts
		result.PeerRequests += lookup.PeerRequests
		completed := false
		errText := ""
		if lookupErr != nil {
			errText = lookupErr.Error()
		} else if lookup.Conflicts == 0 {
			completed, lookupErr = s.store.BinaryEffectiveComplete(ctx, item.BinaryID)
			if lookupErr != nil {
				errText = lookupErr.Error()
			}
		}
		if err := s.store.CompleteBinaryEvidenceRepairWork(ctx, item.BinaryID, completed, errText); err != nil {
			return result, err
		}
		if completed {
			result.Completed++
			gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceCompletedTotal, 1)
		}
	}
	return result, nil
}
