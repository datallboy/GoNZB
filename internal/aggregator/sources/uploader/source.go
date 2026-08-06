package uploader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
	uploaderdomain "github.com/datallboy/gonzb/internal/uploader"
)

type Source struct {
	store uploaderdomain.Store
}

func New(store uploaderdomain.Store) *Source {
	return &Source{store: store}
}

func (s *Source) Name() string { return uploaderdomain.SourceName }

func (s *Source) Search(ctx context.Context, req aggregator.SearchRequest) ([]*domain.Release, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("uploader store is unavailable")
	}
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items, err := s.store.ListSubmissions(ctx, uploaderdomain.ListFilter{
		Query:        req.Query,
		Limit:        min(limit*3, 500),
		ApprovedOnly: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Release, 0, min(len(items), limit))
	for i := range items {
		item := &items[i]
		if !matchesRequest(*item, req) {
			continue
		}
		out = append(out, releaseFromSubmission(*item))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Source) GetByID(ctx context.Context, id string) (*domain.Release, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("uploader store is unavailable")
	}
	item, err := s.store.GetSubmissionByReleaseID(ctx, strings.TrimSpace(id))
	if errors.Is(err, uploaderdomain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item == nil || item.State != uploaderdomain.StateApproved {
		return nil, nil
	}
	return releaseFromSubmission(*item), nil
}

func (s *Source) AuthorizeGet(ctx context.Context, rel *domain.Release) error {
	if rel == nil || strings.TrimSpace(rel.Source) != uploaderdomain.SourceName {
		return uploaderdomain.ErrNotFound
	}
	item, err := s.store.GetSubmission(ctx, strings.TrimSpace(rel.GUID))
	if err != nil && strings.TrimSpace(rel.ID) != "" {
		item, err = s.store.GetSubmissionByReleaseID(ctx, rel.ID)
	}
	if err != nil || item == nil || item.State != uploaderdomain.StateApproved || item.ReleaseID != rel.ID {
		return uploaderdomain.ErrNotFound
	}
	return nil
}

func (s *Source) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	if err := s.AuthorizeGet(ctx, rel); err != nil {
		return nil, err
	}
	item, err := s.store.GetSubmissionByReleaseID(ctx, rel.ID)
	if err != nil {
		return nil, err
	}
	return s.store.OpenNZB(ctx, item.ID)
}

func matchesRequest(item uploaderdomain.Submission, req aggregator.SearchRequest) bool {
	if len(req.Categories) > 0 {
		matched := false
		for _, categoryID := range req.Categories {
			if item.CategoryID == categoryID || (categoryID%1000 == 0 && newsnab.ParentID(item.CategoryID) == categoryID) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if value := strings.TrimSpace(req.IMDbID); value != "" && !strings.EqualFold(strings.TrimPrefix(item.IMDBID, "tt"), strings.TrimPrefix(value, "tt")) {
		return false
	}
	if value := strings.TrimSpace(req.TVDBID); value != "" && strconv.FormatInt(item.TVDBID, 10) != value {
		return false
	}
	return true
}

func releaseFromSubmission(item uploaderdomain.Submission) *domain.Release {
	passwordState := ""
	if item.HasPassword {
		passwordState = "present"
	}
	return &domain.Release{
		ID:              item.ReleaseID,
		GUID:            item.ID,
		Title:           item.Title,
		Password:        passwordState,
		Source:          uploaderdomain.SourceName,
		DownloadURL:     "/nzb/" + item.ReleaseID,
		Size:            item.SizeBytes,
		PublishDate:     item.PostedAt,
		Category:        strconv.Itoa(item.CategoryID),
		RedirectAllowed: false,
		Poster:          item.Poster,
	}
}
