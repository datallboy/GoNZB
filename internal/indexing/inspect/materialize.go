package inspect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type MaterializedBinary struct {
	File         pgindex.CatalogReleaseFile
	Groups       []string
	Header       *nzb.YencHeader
	OutputPath   string
	BytesWritten int64
	ExactSize    int64
	Signature    string
	MIMEType     string
}

var tokenSplitRE = regexp.MustCompile(`[^a-z0-9._-]+`)

func MaterializeBinaryToWorkspace(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, candidate pgindex.BinaryInspectionCandidate, outputPath string, maxBytes int64) (*MaterializedBinary, error) {
	if repo == nil {
		return nil, fmt.Errorf("catalog repo is required")
	}
	if fetcher == nil {
		return nil, fmt.Errorf("article fetcher is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, refs, groups, err := loadBinaryMaterializationInputs(ctx, repo, candidate)
	if err != nil {
		return nil, err
	}
	header, err := fetchYencHeader(ctx, fetcher, groups, refs[0].MessageID)
	if err != nil {
		return nil, err
	}

	exactSize := file.SizeBytes
	if header != nil && header.FileSize > 0 {
		exactSize = header.FileSize
	}
	if exactSize <= 0 {
		exactSize = candidate.TotalBytes
	}
	if exactSize <= 0 {
		return nil, fmt.Errorf("binary %d has no known size", candidate.BinaryID)
	}
	if maxBytes > 0 && exactSize > maxBytes {
		return nil, fmt.Errorf("binary %d materialization %d exceeds inspect max bytes %d", candidate.BinaryID, exactSize, maxBytes)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, fmt.Errorf("create materialized binary dir %s: %w", filepath.Dir(outputPath), err)
	}
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("create materialized binary %s: %w", outputPath, err)
	}
	defer f.Close()
	if err := f.Truncate(exactSize); err != nil {
		return nil, fmt.Errorf("truncate materialized binary %s: %w", outputPath, err)
	}

	var written int64
	var firstBytes []byte
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		article, err := fetchDecodedArticle(ctx, fetcher, groups, ref.MessageID)
		if err != nil {
			return nil, err
		}
		if len(firstBytes) == 0 && len(article.Body) > 0 {
			firstBytes = append(firstBytes, article.Body...)
			if len(firstBytes) > 128 {
				firstBytes = firstBytes[:128]
			}
		}
		if _, err := f.WriteAt(article.Body, article.Offset); err != nil {
			return nil, fmt.Errorf("write materialized binary %s at %d: %w", outputPath, article.Offset, err)
		}
		written += int64(len(article.Body))
	}
	if err := f.Sync(); err != nil {
		return nil, fmt.Errorf("sync materialized binary %s: %w", outputPath, err)
	}

	return &MaterializedBinary{
		File:         file,
		Groups:       groups,
		Header:       header,
		OutputPath:   outputPath,
		BytesWritten: written,
		ExactSize:    exactSize,
		Signature:    DetectSignature(firstBytes, file.FileName),
		MIMEType:     DetectMIMEType(firstBytes, file.FileName),
	}, nil
}

func loadBinaryMaterializationInputs(ctx context.Context, repo CatalogReader, candidate pgindex.BinaryInspectionCandidate) (pgindex.CatalogReleaseFile, []pgindex.CatalogArticleRef, []string, error) {
	files, err := repo.ListCatalogReleaseFiles(ctx, candidate.ReleaseID)
	if err != nil {
		return pgindex.CatalogReleaseFile{}, nil, nil, fmt.Errorf("list catalog release files %s: %w", candidate.ReleaseID, err)
	}
	var file pgindex.CatalogReleaseFile
	found := false
	for _, item := range files {
		if item.BinaryID == candidate.BinaryID {
			file = item
			found = true
			break
		}
	}
	if !found {
		return pgindex.CatalogReleaseFile{}, nil, nil, fmt.Errorf("release %s has no file for binary %d", candidate.ReleaseID, candidate.BinaryID)
	}

	refs, err := repo.ListCatalogReleaseFileArticles(ctx, file.ID)
	if err != nil {
		return pgindex.CatalogReleaseFile{}, nil, nil, fmt.Errorf("list release file articles %d: %w", file.ID, err)
	}
	if len(refs) == 0 {
		return pgindex.CatalogReleaseFile{}, nil, nil, fmt.Errorf("release file %d has no articles", file.ID)
	}

	groups, err := repo.ListCatalogReleaseNewsgroups(ctx, candidate.ReleaseID)
	if err != nil {
		return pgindex.CatalogReleaseFile{}, nil, nil, fmt.Errorf("list release newsgroups %s: %w", candidate.ReleaseID, err)
	}
	if len(groups) == 0 {
		return pgindex.CatalogReleaseFile{}, nil, nil, fmt.Errorf("release %s has no newsgroups", candidate.ReleaseID)
	}

	return file, refs, groups, nil
}

func DetectSignature(data []byte, fileName string) string {
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "png"
	}
	if len(data) >= 6 && string(data[:6]) == "PAR2\x00P" {
		return "par2"
	}
	if len(data) >= 6 && string(data[:6]) == "7z\xbc\xaf'\x1c" {
		return "7z"
	}
	if len(data) >= 4 && string(data[:4]) == "Rar!" {
		return "rar"
	}
	if len(data) >= 4 && string(data[:4]) == "PK\x03\x04" {
		return "zip"
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	switch ext {
	case ".nfo":
		return "text"
	case ".mkv":
		return "matroska"
	case ".mp4", ".m4v":
		return "mp4"
	case ".flac":
		return "flac"
	case ".mp3":
		return "mp3"
	}
	return ""
}

func DetectMIMEType(data []byte, fileName string) string {
	switch DetectSignature(data, fileName) {
	case "par2":
		return "application/x-par2"
	case "7z":
		return "application/x-7z-compressed"
	case "rar":
		return "application/vnd.rar"
	case "zip":
		return "application/zip"
	case "text":
		return "text/plain"
	case "matroska":
		return "video/x-matroska"
	case "mp4":
		return "video/mp4"
	case "flac":
		return "audio/flac"
	case "mp3":
		return "audio/mpeg"
	default:
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
		if ext == ".nfo" {
			return "text/plain"
		}
		return "application/octet-stream"
	}
}

func ExtractTextTokens(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	raw := tokenSplitRE.Split(text, -1)
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if len(token) < 3 {
			continue
		}
		if _, err := strconv.Atoi(token); err == nil {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}
