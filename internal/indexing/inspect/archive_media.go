package inspect

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const (
	defaultArchiveMediaPrefixBytes int64 = 64 * 1024 * 1024
	defaultArchiveMediaOutputBytes int64 = 32 * 1024 * 1024
	minArchiveMediaOutputBytes     int64 = 8 * 1024 * 1024
	sampleArchiveMediaPrefixBytes  int64 = 24 * 1024 * 1024
	sampleArchiveMediaOutputBytes  int64 = 16 * 1024 * 1024
)

type ArchiveMediaMaterialization struct {
	ArchivePath       string
	ArchiveBytes      int64
	ExtractedBytes    int64
	Signature         string
	MIMEType          string
	ExtractStderr     string
	PartialExtraction bool
	FFProbeResult     *FFProbeResult
	FFProbeOutput     []byte
	FFProbeError      string
}

func MaterializeArchiveMediaToWorkspace(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, runner CommandRunner, candidate pgindex.BinaryInspectionCandidate, entryName string, workspaceDir string, opts Options, log logger) (*ArchiveMediaMaterialization, error) {
	entryName = strings.TrimSpace(entryName)
	if entryName == "" {
		return nil, fmt.Errorf("archive entry name is required")
	}
	if repo == nil {
		return nil, fmt.Errorf("catalog repo is required")
	}
	if fetcher == nil {
		return nil, fmt.Errorf("article fetcher is required")
	}

	opts = DefaultOptions(opts)
	lowerFileName := strings.ToLower(strings.TrimSpace(candidate.FileName))

	var archivePath string
	var archiveBytes int64
	switch {
	case splitSevenZipRE.MatchString(lowerFileName):
		archivePath = filepath.Join(workspaceDir, filepath.Base(ArchiveProbePath(candidate.FileName)))
		var err error
		archiveBytes, err = materializeArchiveMediaSevenZip(ctx, repo, fetcher, candidate, archivePath, entryName, opts, log)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("archive-backed media probing is not implemented for %s", candidate.FileName)
	}

	extractedBytes, firstBytes, stderrText, partial, ffprobeResult, ffprobeOutput, ffprobeError, err := probeArchiveEntryStream(ctx, runner, opts.FFProbePath, opts.SevenZipPath, archivePath, entryName, archiveMediaOutputLimit(entryName, opts.MaxBytes), opts.ToolTimeout)
	if err != nil {
		return nil, err
	}
	if extractedBytes <= 0 {
		return nil, fmt.Errorf("archive extraction produced no bytes for %s", entryName)
	}

	return &ArchiveMediaMaterialization{
		ArchivePath:       archivePath,
		ArchiveBytes:      archiveBytes,
		ExtractedBytes:    extractedBytes,
		Signature:         DetectSignature(firstBytes, entryName),
		MIMEType:          DetectMIMEType(firstBytes, entryName),
		ExtractStderr:     strings.TrimSpace(stderrText),
		PartialExtraction: partial,
		FFProbeResult:     ffprobeResult,
		FFProbeOutput:     ffprobeOutput,
		FFProbeError:      strings.TrimSpace(ffprobeError),
	}, nil
}

func materializeArchiveMediaSevenZip(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, candidate pgindex.BinaryInspectionCandidate, archivePath, entryName string, opts Options, log logger) (int64, error) {
	files, err := repo.ListCatalogReleaseFiles(ctx, candidate.ReleaseID)
	if err != nil {
		return 0, fmt.Errorf("list catalog release files %s: %w", candidate.ReleaseID, err)
	}
	family := archiveFamilyFiles(candidate.FileName, files)
	if len(family) == 0 {
		return 0, fmt.Errorf("release %s has no archive family for %s", candidate.ReleaseID, candidate.FileName)
	}
	groups, err := repo.ListCatalogReleaseNewsgroups(ctx, candidate.ReleaseID)
	if err != nil {
		return 0, fmt.Errorf("list catalog release newsgroups %s: %w", candidate.ReleaseID, err)
	}

	probeFiles, err := prepareArchiveProbeFiles(ctx, repo, fetcher, groups, family)
	if err != nil {
		return 0, err
	}

	totalObservedSize := int64(0)
	for _, file := range probeFiles {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		totalObservedSize += file.exactSize
	}
	if totalObservedSize <= 0 {
		return 0, fmt.Errorf("archive family %s has no materializable bytes", family[0].FileName)
	}

	headBytes, headWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, 0, 32)
	if err != nil {
		return headWritten, err
	}
	if headWritten < 32 {
		return headWritten, fmt.Errorf("insufficient bytes for 7z start header: wanted 32 got %d", headWritten)
	}

	nextHeaderStart, nextHeaderEnd, totalArchiveSize, nextHeaderCRC, err := parseSevenZipNextHeaderRange(headBytes)
	if err != nil {
		return headWritten, err
	}
	if totalArchiveSize > totalObservedSize {
		return headWritten, fmt.Errorf("7z header declares archive size %d larger than observed family size %d", totalArchiveSize, totalObservedSize)
	}
	if totalArchiveSize < totalObservedSize {
		return headWritten, fmt.Errorf("7z header declares archive size %d smaller than observed family size %d", totalArchiveSize, totalObservedSize)
	}

	if err := createSparseArchiveFile(archivePath, totalArchiveSize); err != nil {
		return 0, err
	}

	prefixBytesToRead := archiveMediaPrefixLimit(entryName, totalArchiveSize, opts.MaxBytes)
	prefixBytes, prefixWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, 0, prefixBytesToRead)
	if err != nil {
		return prefixWritten, err
	}
	prefixMaterialized, err := writeSparseArchiveFileRange(archivePath, 0, prefixBytes)
	if err != nil {
		return prefixMaterialized, err
	}

	nextHeaderBytes, nextWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, nextHeaderStart, nextHeaderEnd)
	if err != nil {
		return prefixMaterialized + nextWritten, err
	}
	expectedNextHeaderBytes := nextHeaderEnd - nextHeaderStart
	if nextWritten < expectedNextHeaderBytes {
		return prefixMaterialized + nextWritten, fmt.Errorf("insufficient bytes for 7z next header: wanted %d got %d", expectedNextHeaderBytes, nextWritten)
	}
	if actualCRC := crc32.ChecksumIEEE(nextHeaderBytes); actualCRC != nextHeaderCRC {
		return prefixMaterialized + nextWritten, fmt.Errorf("7z next header crc mismatch: expected %08X got %08X", nextHeaderCRC, actualCRC)
	}
	nextHeaderMaterialized, err := writeSparseArchiveFileRange(archivePath, nextHeaderStart, nextHeaderBytes)
	if err != nil {
		return prefixMaterialized + nextHeaderMaterialized, err
	}

	totalWritten := prefixMaterialized + nextHeaderMaterialized
	encodedHeaderStart, encodedHeaderEnd, hasEncodedHeader, err := parseSevenZipEncodedHeaderPackRange(nextHeaderBytes)
	if err != nil {
		return totalWritten, err
	}
	if hasEncodedHeader {
		absoluteEncodedStart := int64(32) + encodedHeaderStart
		absoluteEncodedEnd := int64(32) + encodedHeaderEnd
		encodedHeaderBytes, encodedWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, absoluteEncodedStart, absoluteEncodedEnd)
		totalWritten += encodedWritten
		if err != nil {
			return totalWritten, err
		}
		if encodedWritten < absoluteEncodedEnd-absoluteEncodedStart {
			return totalWritten, fmt.Errorf("insufficient bytes for 7z encoded header: wanted %d got %d", absoluteEncodedEnd-absoluteEncodedStart, encodedWritten)
		}
		wrote, err := writeSparseArchiveFileRange(archivePath, absoluteEncodedStart, encodedHeaderBytes)
		totalWritten += wrote
		if err != nil {
			return totalWritten, err
		}
	}

	if log != nil {
		log.Info(
			"inspect_media: prepared archive media probe binary_id=%d release_id=%s entry=%q prefix_bytes=%d archive_bytes=%d probe=%s",
			candidate.BinaryID,
			candidate.ReleaseID,
			entryName,
			prefixBytesToRead,
			totalWritten,
			archivePath,
		)
	}

	return totalWritten, nil
}

func probeArchiveEntryStream(ctx context.Context, runner CommandRunner, ffprobePath, sevenZipPath, archivePath, entryName string, outputLimit int64, timeout time.Duration) (int64, []byte, string, bool, *FFProbeResult, []byte, string, error) {
	if strings.TrimSpace(sevenZipPath) == "" {
		return 0, nil, "", false, nil, nil, "", fmt.Errorf("7z path is required")
	}
	matroskaEntry := isMatroskaEntryName(entryName)
	if runner == nil && !matroskaEntry {
		return 0, nil, "", false, nil, nil, "", fmt.Errorf("ffprobe runner is required")
	}
	if outputLimit <= 0 {
		outputLimit = defaultArchiveMediaOutputBytes
	}

	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(toolCtx, sevenZipPath, "x", "-so", "-y", archivePath, entryName)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, "", false, nil, nil, "", fmt.Errorf("open archive extraction stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return 0, nil, "", false, nil, nil, "", fmt.Errorf("start archive extraction: %w", err)
	}

	if matroskaEntry {
		sampler := &countingPreviewReader{r: io.LimitReader(stdout, outputLimit)}
		result, parseErr := ParseMatroskaHeaderReader(sampler, 0)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		if parseErr != nil {
			return sampler.n, sampler.preview(), stderr.String(), true, nil, nil, "", fmt.Errorf("parse Matroska archive entry header %q: %w", entryName, parseErr)
		}
		return sampler.n, sampler.preview(), stderr.String(), true, result, nil, "", nil
	}

	payload, readErr := io.ReadAll(io.LimitReader(stdout, outputLimit+1))
	partial := int64(len(payload)) > outputLimit
	if partial {
		payload = payload[:outputLimit]
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return int64(len(payload)), previewBytes(payload), stderr.String(), partial, nil, nil, "", fmt.Errorf("read archive entry %q prefix: %w", entryName, readErr)
	}
	if waitErr != nil {
		partial = true
		if len(payload) == 0 {
			return 0, nil, stderr.String(), partial, nil, nil, "", fmt.Errorf("extract archive entry %q from %s: %w", entryName, archivePath, waitErr)
		}
	}
	ffprobeResult, ffprobeOutput, probeErr := RunFFProbeInput(toolCtx, runner, ffprobePath, bytes.NewReader(payload))
	if probeErr != nil {
		return int64(len(payload)), previewBytes(payload), stderr.String(), partial, ffprobeResult, ffprobeOutput, probeErr.Error(), nil
	}
	return int64(len(payload)), previewBytes(payload), stderr.String(), partial, ffprobeResult, ffprobeOutput, "", nil
}

func isMatroskaEntryName(entryName string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(entryName))) {
	case ".mkv", ".webm", ".mka", ".mks", ".mk3d":
		return true
	default:
		return false
	}
}

func previewBytes(payload []byte) []byte {
	if len(payload) > 128 {
		payload = payload[:128]
	}
	return append([]byte(nil), payload...)
}

type countingPreviewReader struct {
	r   io.Reader
	n   int64
	buf [128]byte
	got int
}

func (r *countingPreviewReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		if r.got < len(r.buf) {
			copied := copy(r.buf[r.got:], p[:n])
			r.got += copied
		}
		r.n += int64(n)
	}
	return n, err
}

func (r *countingPreviewReader) preview() []byte {
	if r == nil || r.got == 0 {
		return nil
	}
	return append([]byte(nil), r.buf[:r.got]...)
}

func archiveMediaPrefixLimit(entryName string, totalArchiveSize, maxBytes int64) int64 {
	limit := defaultArchiveMediaPrefixBytes
	lower := strings.ToLower(strings.TrimSpace(entryName))
	if strings.Contains(lower, "/sample/") || strings.Contains(lower, ".sample.") || strings.Contains(lower, "sample-") {
		limit = sampleArchiveMediaPrefixBytes
	}
	if maxBytes > 0 && maxBytes < limit {
		limit = maxBytes
	}
	if totalArchiveSize > 0 && totalArchiveSize < limit {
		return totalArchiveSize
	}
	return limit
}

func archiveMediaOutputLimit(entryName string, maxBytes int64) int64 {
	limit := defaultArchiveMediaOutputBytes
	lower := strings.ToLower(strings.TrimSpace(entryName))
	if strings.Contains(lower, "/sample/") || strings.Contains(lower, ".sample.") || strings.Contains(lower, "sample-") {
		limit = sampleArchiveMediaOutputBytes
	}
	if maxBytes > 0 && maxBytes < limit {
		limit = maxBytes
	}
	if limit < minArchiveMediaOutputBytes {
		limit = minArchiveMediaOutputBytes
	}
	return limit
}
