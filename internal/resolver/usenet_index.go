package resolver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type usenetIndexCatalog interface {
	GetCatalogReleaseByID(ctx context.Context, releaseID string) (*domain.Release, error)
	ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]pgindex.CatalogReleaseFile, error)
	ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]pgindex.CatalogArticleRef, error)
	ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error)
	UpsertNZBCache(ctx context.Context, releaseID, generationStatus, hashSHA256, lastError string) error
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

// build NZB on demand from PG release_files + release_file_articles.
func (r *usenetIndexResolver) GetNZB(ctx context.Context, rel *domain.Release) (io.ReadCloser, error) {
	if r.catalog == nil {
		return nil, fmt.Errorf("usenet index catalog is not configured")
	}
	if rel == nil || strings.TrimSpace(rel.ID) == "" {
		return nil, fmt.Errorf("usenet index release is required")
	}

	files, err := r.catalog.ListCatalogReleaseFiles(ctx, rel.ID)
	if err != nil {
		return nil, fmt.Errorf("load catalog release files for %s: %w", rel.ID, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no release files found for %s", rel.ID)
	}

	groups, err := r.catalog.ListCatalogReleaseNewsgroups(ctx, rel.ID)
	if err != nil {
		return nil, fmt.Errorf("load catalog release newsgroups for %s: %w", rel.ID, err)
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("no release newsgroups found for %s", rel.ID)
	}

	nzbBytes, err := r.buildNZB(ctx, rel, files, groups)
	if err != nil {
		_ = r.catalog.UpsertNZBCache(ctx, rel.ID, "failed", "", err.Error())
		return nil, err
	}

	hash := sha256.Sum256(nzbBytes)
	if err := r.catalog.UpsertNZBCache(ctx, rel.ID, "ready", hex.EncodeToString(hash[:]), ""); err != nil {
		return nil, fmt.Errorf("update nzb cache metadata for %s: %w", rel.ID, err)
	}

	return io.NopCloser(bytes.NewReader(nzbBytes)), nil
}

func (r *usenetIndexResolver) buildNZB(ctx context.Context, rel *domain.Release, files []pgindex.CatalogReleaseFile, groups []string) ([]byte, error) {
	type segmentXML struct {
		Bytes  int64  `xml:"bytes,attr"`
		Number int    `xml:"number,attr"`
		ID     string `xml:",chardata"`
	}

	type groupXML struct {
		Name string `xml:",chardata"`
	}

	type fileXML struct {
		Poster   string       `xml:"poster,attr"`
		Date     int64        `xml:"date,attr"`
		Subject  string       `xml:"subject,attr"`
		Groups   []groupXML   `xml:"groups>group"`
		Segments []segmentXML `xml:"segments>segment"`
	}

	type nzbXML struct {
		XMLName xml.Name  `xml:"nzb"`
		Xmlns   string    `xml:"xmlns,attr"`
		Files   []fileXML `xml:"file"`
	}

	sort.Strings(groups)

	doc := nzbXML{
		Xmlns: "http://www.newzbin.com/DTD/2003/nzb",
		Files: make([]fileXML, 0, len(files)),
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		articles, err := r.catalog.ListCatalogReleaseFileArticles(ctx, f.ID)
		if err != nil {
			return nil, fmt.Errorf("load release file articles %d: %w", f.ID, err)
		}
		if len(articles) == 0 {
			continue
		}

		segments := make([]segmentXML, 0, len(articles))
		for _, article := range articles {
			msgID := strings.TrimSpace(article.MessageID)
			if msgID == "" {
				continue
			}
			segments = append(segments, segmentXML{
				Bytes:  article.Bytes,
				Number: article.PartNumber,
				ID:     msgID,
			})
		}
		if len(segments) == 0 {
			continue
		}

		subject := strings.TrimSpace(f.Subject)
		if subject == "" {
			subject = strings.TrimSpace(f.FileName)
		}
		if subject == "" {
			subject = rel.Title
		}

		poster := strings.TrimSpace(f.Poster)
		if poster == "" {
			poster = rel.Poster
		}
		if poster == "" {
			poster = "unknown"
		}

		dateUnix := rel.PublishDate.Unix()
		if f.PostedAt != nil && !f.PostedAt.IsZero() {
			dateUnix = f.PostedAt.UTC().Unix()
		}

		fileGroups := make([]groupXML, 0, len(groups))
		for _, group := range groups {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			fileGroups = append(fileGroups, groupXML{Name: group})
		}

		doc.Files = append(doc.Files, fileXML{
			Poster:   poster,
			Date:     dateUnix,
			Subject:  subject,
			Groups:   fileGroups,
			Segments: segments,
		})
	}

	if len(doc.Files) == 0 {
		return nil, fmt.Errorf("release %s produced no nzb files", rel.ID)
	}

	payload, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal nzb xml for %s: %w", rel.ID, err)
	}

	return append([]byte(xml.Header), payload...), nil
}
