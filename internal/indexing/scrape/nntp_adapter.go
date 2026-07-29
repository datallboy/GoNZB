package scrape

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/nntp"
)

type nntpClient interface {
	ID() string
	GroupStats(ctx context.Context, group string) (nntp.GroupStats, error)
	XOver(ctx context.Context, group string, from, to int64) ([]nntp.OverviewHeader, error)
}

type providerAwareNNTPClient interface {
	GroupStatsWithProvider(ctx context.Context, group string) (nntp.GroupStats, string, error)
	XOverWithProvider(ctx context.Context, group string, from, to int64) ([]nntp.OverviewHeader, string, error)
}

type providerPinnedNNTPClient interface {
	XOverOnProvider(ctx context.Context, providerID, group string, from, to int64) ([]nntp.OverviewHeader, string, error)
}

type NNTPAdapter struct {
	p nntpClient
}

func NewNNTPAdapter(p nntpClient) *NNTPAdapter {
	return &NNTPAdapter{p: p}
}

func (a *NNTPAdapter) ID() string {
	return a.p.ID()
}

func (a *NNTPAdapter) GroupStats(ctx context.Context, group string) (GroupStats, error) {
	if aware, ok := a.p.(providerAwareNNTPClient); ok {
		gs, providerID, err := aware.GroupStatsWithProvider(ctx, group)
		if err != nil {
			return GroupStats{}, err
		}
		return GroupStats{
			Low:        gs.Low,
			High:       gs.High,
			ProviderID: providerID,
		}, nil
	}
	gs, err := a.p.GroupStats(ctx, group)
	if err != nil {
		return GroupStats{}, err
	}
	return GroupStats{
		Low:  gs.Low,
		High: gs.High,
	}, nil
}

func (a *NNTPAdapter) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	rows, _, err := a.XOverWithProvider(ctx, group, from, to)
	return rows, err
}

func (a *NNTPAdapter) XOverWithProvider(ctx context.Context, group string, from, to int64) ([]OverviewHeader, string, error) {
	var (
		rows       []nntp.OverviewHeader
		providerID string
		err        error
	)
	if aware, ok := a.p.(providerAwareNNTPClient); ok {
		rows, providerID, err = aware.XOverWithProvider(ctx, group, from, to)
	} else {
		rows, err = a.p.XOver(ctx, group, from, to)
		providerID = a.ID()
	}
	if err != nil {
		return nil, "", err
	}

	return mapOverviewHeaders(rows), providerID, nil
}

func (a *NNTPAdapter) XOverOnProvider(ctx context.Context, providerID, group string, from, to int64) ([]OverviewHeader, string, error) {
	pinned, ok := a.p.(providerPinnedNNTPClient)
	if !ok {
		return nil, "", fmt.Errorf("NNTP client does not support provider-pinned XOVER")
	}
	rows, actualProviderID, err := pinned.XOverOnProvider(ctx, providerID, group, from, to)
	if err != nil {
		return nil, actualProviderID, err
	}
	return mapOverviewHeaders(rows), actualProviderID, nil
}

func mapOverviewHeaders(rows []nntp.OverviewHeader) []OverviewHeader {
	out := make([]OverviewHeader, 0, len(rows))
	for _, r := range rows {
		out = append(out, OverviewHeader{
			ArticleNumber: r.ArticleNumber,
			MessageID:     r.MessageID,
			Subject:       r.Subject,
			Poster:        r.Poster,
			DateUTC:       r.DateUTC,
			Bytes:         r.Bytes,
			Lines:         r.Lines,
			Xref:          r.Xref,
			RawOverview:   r.RawOverview,
		})
	}
	return out
}
