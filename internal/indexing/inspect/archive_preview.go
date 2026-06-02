package inspect

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func MaterializeArchivePreviewImage(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, runner CommandRunner, candidate pgindex.BinaryInspectionCandidate, entryName, workspaceDir string, opts Options, log logger) ([]byte, string, error) {
	entryName = strings.TrimSpace(entryName)
	if entryName == "" {
		return nil, "", fmt.Errorf("archive entry name is required")
	}
	archivePath := filepath.Join(workspaceDir, filepath.Base(ArchiveProbePath(candidate.FileName)))
	if _, err := materializeArchiveMediaSevenZip(ctx, repo, fetcher, candidate, archivePath, entryName, opts, log); err != nil {
		return nil, "", err
	}
	output, err := runner.Run(ctx, opts.SevenZipPath, "x", "-so", "-y", archivePath, entryName)
	if err != nil {
		return nil, "", fmt.Errorf("extract archive preview image %q: %w", entryName, err)
	}
	return output, DetectMIMEType(output, entryName), nil
}

func MaterializeArchiveVideoThumbnail(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, runner CommandRunner, candidate pgindex.BinaryInspectionCandidate, entryName, workspaceDir string, opts Options, log logger) ([]byte, error) {
	entryName = strings.TrimSpace(entryName)
	if entryName == "" {
		return nil, fmt.Errorf("archive entry name is required")
	}
	archivePath := filepath.Join(workspaceDir, filepath.Base(ArchiveProbePath(candidate.FileName)))
	if _, err := materializeArchiveMediaSevenZip(ctx, repo, fetcher, candidate, archivePath, entryName, opts, log); err != nil {
		return nil, err
	}
	output, err := runner.Run(ctx, opts.SevenZipPath, "x", "-so", "-y", archivePath, entryName)
	if err != nil {
		return nil, fmt.Errorf("extract archive media %q for thumbnail: %w", entryName, err)
	}
	return RunFFmpegThumbnailInput(ctx, runner, opts.FFmpegPath, bytes.NewReader(output))
}
