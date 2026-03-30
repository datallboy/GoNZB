package scrape

import (
	"context"

	"github.com/datallboy/gonzb/internal/nntp"
)

type NNTPAdapter struct {
	p nntp.Provider
}

func NewNNTPAdapter(p nntp.Provider) *NNTPAdapter {
	return &NNTPAdapter{p: p}
}

func (a *NNTPAdapter) ID() string {
	return a.p.ID()
}

func (a *NNTPAdapter) GroupStats(ctx context.Context, group string) (GroupStats, error) {
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
	rows, err := a.p.XOver(ctx, group, from, to)
	if err != nil {
		return nil, err
	}

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
	return out, nil
}
