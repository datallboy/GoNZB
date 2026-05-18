package inspect

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type CatalogReader interface {
	ListCatalogReleaseFiles(ctx context.Context, releaseID string) ([]pgindex.CatalogReleaseFile, error)
	ListCatalogReleaseFileArticles(ctx context.Context, releaseFileID int64) ([]pgindex.CatalogArticleRef, error)
	ListCatalogReleaseNewsgroups(ctx context.Context, releaseID string) ([]string, error)
}

type ArticleFetcher interface {
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	RunInput(ctx context.Context, input io.Reader, name string, args ...string) ([]byte, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (ExecCommandRunner) RunInput(ctx context.Context, input io.Reader, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = input
	return cmd.CombinedOutput()
}

type ArchiveProbeResult struct {
	Strategy          string
	ProbePath         string
	MaterializedBytes int64
	Entries           []ArchiveEntryInfo
	EntryNames        []string
	Encrypted         bool
	ProbeError        string
	FamilyFileNames   []string
}

type ArchiveEntryInfo struct {
	Name             string
	IsDir            bool
	UncompressedSize int64
	CompressedSize   int64
	Encrypted        bool
	Comment          string
}

type archiveProbeFile struct {
	file      pgindex.CatalogReleaseFile
	refs      []pgindex.CatalogArticleRef
	exactSize int64
}

func PrepareArchiveProbe(ctx context.Context, workspace *Workspace, repo CatalogReader, fetcher ArticleFetcher, runner CommandRunner, log logger, opts Options, candidate pgindex.BinaryInspectionCandidate) (*ArchiveProbeResult, error) {
	result := &ArchiveProbeResult{}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if workspace == nil || repo == nil {
		return result, nil
	}

	files, err := repo.ListCatalogReleaseFiles(ctx, candidate.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release files %s: %w", candidate.ReleaseID, err)
	}

	family := archiveFamilyFiles(candidate.FileName, files)
	if len(family) == 0 {
		result.Strategy = "no_archive_family"
		return result, nil
	}
	result.FamilyFileNames = make([]string, 0, len(family))
	for _, file := range family {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result.FamilyFileNames = append(result.FamilyFileNames, file.FileName)
	}

	if fetcher == nil {
		result.Strategy = "metadata_only"
		return result, nil
	}

	groups, err := repo.ListCatalogReleaseNewsgroups(ctx, candidate.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("list catalog release newsgroups %s: %w", candidate.ReleaseID, err)
	}

	switch {
	case splitSevenZipRE.MatchString(strings.ToLower(strings.TrimSpace(candidate.FileName))):
		result.Strategy = "sparse_combined_7z"
		result.ProbePath = filepath.Join(workspace.Dir, filepath.Base(ArchiveProbePath(candidate.FileName)))
		if log != nil {
			log.Debug("inspect_archive: materializing sparse combined 7z binary_id=%d release_id=%s files=%d probe=%s", candidate.BinaryID, candidate.ReleaseID, len(family), result.ProbePath)
		}
		result.MaterializedBytes, err = materializeSparseSplitArchive(ctx, repo, fetcher, groups, family, result.ProbePath, log, candidate, opts)
	default:
		result.ProbePath = filepath.Join(workspace.Dir, filepath.Base(ArchiveProbePath(candidate.FileName)))
		result.Strategy = "leading_archive_header"
		if log != nil {
			log.Debug("inspect_archive: materializing leading header binary_id=%d release_id=%s probe=%s", candidate.BinaryID, candidate.ReleaseID, result.ProbePath)
		}
		result.MaterializedBytes, err = materializeLeadingArchive(ctx, repo, fetcher, groups, family[0], result.ProbePath)
	}
	if err != nil {
		result.ProbeError = err.Error()
		return result, nil
	}

	if runner == nil || strings.TrimSpace(opts.SevenZipPath) == "" {
		return result, nil
	}

	toolCtx, cancel := context.WithTimeout(ctx, opts.ToolTimeout)
	defer cancel()
	if log != nil {
		log.Debug("inspect_archive: invoking 7z binary_id=%d release_id=%s timeout=%s probe=%s", candidate.BinaryID, candidate.ReleaseID, opts.ToolTimeout, result.ProbePath)
	}
	output, err := runner.Run(toolCtx, opts.SevenZipPath, "l", "-slt", result.ProbePath)
	result.Entries, result.Encrypted = parseSevenZipListingDetails(output, result.ProbePath)
	result.EntryNames = archiveEntryNames(result.Entries)
	if err != nil && result.ProbeError == "" {
		result.ProbeError = strings.TrimSpace(string(output))
		if result.ProbeError == "" {
			result.ProbeError = err.Error()
		}
	}
	return result, nil
}

func archiveFamilyFiles(candidateFile string, files []pgindex.CatalogReleaseFile) []pgindex.CatalogReleaseFile {
	return ArchiveFamilyFiles(candidateFile, files)
}

func materializeSparseSplitArchive(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, groups []string, family []pgindex.CatalogReleaseFile, probePath string, log logger, candidate pgindex.BinaryInspectionCandidate, opts Options) (int64, error) {
	probeFiles, err := prepareArchiveProbeFiles(ctx, repo, fetcher, groups, family)
	if err != nil {
		return 0, err
	}

	totalObservedSize := int64(0)
	for _, file := range probeFiles {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if file.exactSize > 0 {
			totalObservedSize += file.exactSize
		}
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
	if log != nil {
		log.Debug(
			"inspect_archive: parsed 7z header binary_id=%d release_id=%s next_header_start=%d next_header_end=%d next_header_bytes=%d total_archive_size=%d observed_family_size=%d overlap_files=%s",
			candidate.BinaryID,
			candidate.ReleaseID,
			nextHeaderStart,
			nextHeaderEnd,
			nextHeaderEnd-nextHeaderStart,
			totalArchiveSize,
			totalObservedSize,
			strings.Join(overlappingArchiveFiles(probeFiles, nextHeaderStart, nextHeaderEnd), ","),
		)
	}
	if totalArchiveSize > totalObservedSize {
		return headWritten, fmt.Errorf("7z header declares archive size %d larger than observed family size %d", totalArchiveSize, totalObservedSize)
	}
	if totalArchiveSize < totalObservedSize {
		return headWritten, fmt.Errorf("7z header declares archive size %d smaller than observed family size %d", totalArchiveSize, totalObservedSize)
	}
	nextHeaderBytesToRead := nextHeaderEnd - nextHeaderStart
	if opts.MaxBytes > 0 && 32+nextHeaderBytesToRead > opts.MaxBytes {
		return headWritten, fmt.Errorf("7z next header materialization %d exceeds inspect max bytes %d", 32+nextHeaderBytesToRead, opts.MaxBytes)
	}
	if err := createSparseArchiveFile(probePath, totalArchiveSize); err != nil {
		return 0, err
	}
	if log != nil {
		log.Debug(
			"inspect_archive: created sparse combined 7z binary_id=%d release_id=%s size=%d",
			candidate.BinaryID,
			candidate.ReleaseID,
			totalArchiveSize,
		)
	}

	firstArticle, err := fetchDecodedArticle(ctx, fetcher, groups, probeFiles[0].refs[0].MessageID)
	if err != nil {
		return 0, err
	}
	written, err := writeSparseArchiveFileRange(probePath, firstArticle.Offset, firstArticle.Body)
	if err != nil {
		return written, err
	}
	if log != nil {
		log.Debug(
			"inspect_archive: materialized leading article binary_id=%d release_id=%s file=%s bytes=%d",
			candidate.BinaryID,
			candidate.ReleaseID,
			probeFiles[0].file.FileName,
			written,
		)
	}

	nextHeaderBytes, nextWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, nextHeaderStart, nextHeaderEnd)
	if err != nil {
		return written + nextWritten, err
	}
	if log != nil {
		log.Debug(
			"inspect_archive: fetched next header bytes binary_id=%d release_id=%s fetched=%d expected=%d",
			candidate.BinaryID,
			candidate.ReleaseID,
			nextWritten,
			nextHeaderBytesToRead,
		)
	}
	if nextWritten < nextHeaderBytesToRead {
		return written + nextWritten, fmt.Errorf("insufficient bytes for 7z next header: wanted %d got %d", nextHeaderBytesToRead, nextWritten)
	}
	actualCRC := crc32.ChecksumIEEE(nextHeaderBytes)
	if actualCRC != nextHeaderCRC {
		return written + nextWritten, fmt.Errorf("7z next header crc mismatch: expected %08X got %08X", nextHeaderCRC, actualCRC)
	}
	nextHeaderWritten, err := writeSparseArchiveFileRange(probePath, nextHeaderStart, nextHeaderBytes)
	if err != nil {
		return written + nextWritten, err
	}

	totalWritten := written + nextHeaderWritten
	encodedHeaderStart, encodedHeaderEnd, hasEncodedHeader, err := parseSevenZipEncodedHeaderPackRange(nextHeaderBytes)
	if err != nil {
		return totalWritten, err
	}
	if hasEncodedHeader {
		absoluteEncodedStart := int64(32) + encodedHeaderStart
		absoluteEncodedEnd := int64(32) + encodedHeaderEnd
		if log != nil {
			log.Debug(
				"inspect_archive: encoded header pack range binary_id=%d release_id=%s start=%d end=%d bytes=%d overlap_files=%s",
				candidate.BinaryID,
				candidate.ReleaseID,
				absoluteEncodedStart,
				absoluteEncodedEnd,
				absoluteEncodedEnd-absoluteEncodedStart,
				strings.Join(overlappingArchiveFiles(probeFiles, absoluteEncodedStart, absoluteEncodedEnd), ","),
			)
		}
		encodedHeaderBytes, encodedWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, absoluteEncodedStart, absoluteEncodedEnd)
		totalWritten += encodedWritten
		if err != nil {
			return totalWritten, err
		}
		if encodedWritten < absoluteEncodedEnd-absoluteEncodedStart {
			return totalWritten, fmt.Errorf("insufficient bytes for 7z encoded header: wanted %d got %d", absoluteEncodedEnd-absoluteEncodedStart, encodedWritten)
		}
		wrote, err := writeSparseArchiveFileRange(probePath, absoluteEncodedStart, encodedHeaderBytes)
		totalWritten += wrote
		if err != nil {
			return totalWritten, err
		}
	}
	if log != nil {
		log.Debug(
			"inspect_archive: materialized sparse combined 7z segments binary_id=%d release_id=%s bytes=%d",
			candidate.BinaryID,
			candidate.ReleaseID,
			totalWritten,
		)
	}
	return totalWritten, nil
}

func materializeSparseSplitArchiveVolumes(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, groups []string, family []pgindex.CatalogReleaseFile, workspaceDir string, log logger, candidate pgindex.BinaryInspectionCandidate, opts Options) (int64, error) {
	probeFiles, err := prepareArchiveProbeFiles(ctx, repo, fetcher, groups, family)
	if err != nil {
		return 0, err
	}

	totalObservedSize := int64(0)
	for _, file := range probeFiles {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if file.exactSize > 0 {
			totalObservedSize += file.exactSize
		}
	}
	if totalObservedSize <= 0 {
		return 0, fmt.Errorf("archive family %s has no materializable bytes", family[0].FileName)
	}

	headBytes, headWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, 0, 32)
	if err != nil {
		return headWritten, err
	}
	if headWritten < 32 {
		return headWritten, fmt.Errorf("insufficient bytes for archive start header: wanted 32 got %d", headWritten)
	}
	nextHeaderStart, nextHeaderEnd, totalArchiveSize, _, err := parseSevenZipNextHeaderRange(headBytes)
	if err != nil {
		return headWritten, err
	}
	if totalArchiveSize > totalObservedSize {
		return headWritten, fmt.Errorf("archive header declares archive size %d larger than observed family size %d", totalArchiveSize, totalObservedSize)
	}
	nextHeaderBytesToRead := nextHeaderEnd - nextHeaderStart
	if opts.MaxBytes > 0 && 32+nextHeaderBytesToRead > opts.MaxBytes {
		return headWritten, fmt.Errorf("archive next header materialization %d exceeds inspect max bytes %d", 32+nextHeaderBytesToRead, opts.MaxBytes)
	}
	if err := createSparseArchiveFamily(workspaceDir, probeFiles); err != nil {
		return 0, err
	}

	nextHeaderBytes, nextWritten, err := readArchiveRange(ctx, fetcher, groups, probeFiles, nextHeaderStart, nextHeaderEnd)
	if err != nil {
		return headWritten + nextWritten, err
	}
	if nextWritten < nextHeaderBytesToRead {
		return headWritten + nextWritten, fmt.Errorf("insufficient bytes for archive next header: wanted %d got %d", nextHeaderBytesToRead, nextWritten)
	}
	written, err := writeArchiveFamilyRange(workspaceDir, probeFiles, 0, headBytes)
	if err != nil {
		return written, err
	}
	nextWrittenBytes, err := writeArchiveFamilyRange(workspaceDir, probeFiles, nextHeaderStart, nextHeaderBytes)
	return written + nextWrittenBytes, err
}

func overlappingArchiveFiles(family []archiveProbeFile, start, end int64) []string {
	if end <= start {
		return nil
	}
	offset := int64(0)
	out := make([]string, 0, 4)
	for _, file := range family {
		fileStart := offset
		fileEnd := fileStart + file.exactSize
		offset = fileEnd
		if minInt64(end, fileEnd) <= maxInt64(start, fileStart) {
			continue
		}
		out = append(out, filepath.Base(strings.TrimSpace(file.file.FileName)))
	}
	return out
}

func materializeLeadingArchive(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, groups []string, file pgindex.CatalogReleaseFile, probePath string) (int64, error) {
	body, err := collectBoundaryArticles(ctx, repo, fetcher, groups, file.ID, false, 2)
	if err != nil {
		return 0, err
	}
	if len(body) == 0 {
		return 0, fmt.Errorf("no archive bytes materialized for %s", file.FileName)
	}
	if err := os.WriteFile(probePath, body, 0644); err != nil {
		return 0, fmt.Errorf("write archive probe %s: %w", probePath, err)
	}
	return int64(len(body)), nil
}

func createSparseArchiveFamily(workspaceDir string, family []archiveProbeFile) error {
	for _, file := range family {
		path := filepath.Join(workspaceDir, filepath.Base(strings.TrimSpace(file.file.FileName)))
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create sparse archive volume %s: %w", path, err)
		}
		if err := f.Truncate(file.exactSize); err != nil {
			f.Close()
			return fmt.Errorf("truncate sparse archive volume %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close sparse archive volume %s: %w", path, err)
		}
	}
	return nil
}

func createSparseArchiveFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create sparse archive file %s: %w", path, err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return fmt.Errorf("truncate sparse archive file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close sparse archive file %s: %w", path, err)
	}
	return nil
}

func writeArchiveFamilyRange(workspaceDir string, family []archiveProbeFile, start int64, data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	end := start + int64(len(data))
	globalOffset := int64(0)
	written := int64(0)

	for _, file := range family {
		fileStart := globalOffset
		fileEnd := fileStart + file.exactSize
		globalOffset = fileEnd

		overlapStart := maxInt64(start, fileStart)
		overlapEnd := minInt64(end, fileEnd)
		if overlapEnd <= overlapStart {
			continue
		}

		path := filepath.Join(workspaceDir, filepath.Base(strings.TrimSpace(file.file.FileName)))
		f, err := os.OpenFile(path, os.O_WRONLY, 0644)
		if err != nil {
			return written, fmt.Errorf("open sparse archive volume %s: %w", path, err)
		}

		srcStart := overlapStart - start
		srcEnd := overlapEnd - start
		dstOffset := overlapStart - fileStart
		if _, err := f.WriteAt(data[srcStart:srcEnd], dstOffset); err != nil {
			f.Close()
			return written, fmt.Errorf("write sparse archive volume %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return written, fmt.Errorf("close sparse archive volume %s: %w", path, err)
		}
		written += overlapEnd - overlapStart
	}

	return written, nil
}

func writeSparseArchiveFileRange(path string, start int64, data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("open sparse archive file %s: %w", path, err)
	}
	if _, err := f.WriteAt(data, start); err != nil {
		f.Close()
		return 0, fmt.Errorf("write sparse archive file %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("close sparse archive file %s: %w", path, err)
	}
	return int64(len(data)), nil
}

func materializeArchiveProbeFile(ctx context.Context, fetcher ArticleFetcher, groups []string, probePath string, baseOffset int64, file archiveProbeFile) (int64, error) {
	var written int64
	for _, ref := range file.refs {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		article, err := fetchDecodedArticle(ctx, fetcher, groups, ref.MessageID)
		if err != nil {
			return written, err
		}
		wrote, err := writeSparseArchiveFileRange(probePath, baseOffset+article.Offset, article.Body)
		written += wrote
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func trailingProbeStart(family []archiveProbeFile, absoluteStart int64) (int, int64) {
	offset := int64(0)
	for idx, file := range family {
		fileStart := offset
		fileEnd := fileStart + file.exactSize
		offset = fileEnd
		if absoluteStart < fileEnd {
			return idx, fileStart
		}
	}
	if len(family) == 0 {
		return 0, 0
	}
	last := len(family) - 1
	return last, trailingStartOffsetForIndex(family, last)
}

func trailingStartOffsetForIndex(family []archiveProbeFile, index int) int64 {
	if index <= 0 {
		return 0
	}
	offset := int64(0)
	for idx := 0; idx < len(family) && idx < index; idx++ {
		offset += family[idx].exactSize
	}
	return offset
}

func collectBoundaryArticles(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, groups []string, releaseFileID int64, fromEnd bool, count int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	refs, err := repo.ListCatalogReleaseFileArticles(ctx, releaseFileID)
	if err != nil {
		return nil, fmt.Errorf("list release file articles %d: %w", releaseFileID, err)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("release file %d has no articles", releaseFileID)
	}
	if count <= 0 || count > len(refs) {
		count = len(refs)
	}

	selected := refs[:count]
	if fromEnd {
		selected = refs[len(refs)-count:]
	}

	var out []byte
	for _, ref := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		article, err := fetchDecodedArticle(ctx, fetcher, groups, ref.MessageID)
		if err != nil {
			return nil, err
		}
		out = append(out, article.Body...)
	}
	return out, nil
}

type decodedArticle struct {
	Offset   int64
	FileSize int64
	Body     []byte
}

func fetchDecodedArticle(ctx context.Context, fetcher ArticleFetcher, groups []string, messageID string) (*decodedArticle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := fetcher.Fetch(ctx, messageID, groups)
	if err != nil {
		return nil, fmt.Errorf("fetch article %s: %w", messageID, err)
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
	decoder := nzb.NewYencDecoder(reader)
	if err := decoder.DiscardHeader(); err != nil {
		return nil, fmt.Errorf("decode article %s header: %w", messageID, err)
	}
	body, err := io.ReadAll(decoder)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("decode article %s body: %w", messageID, err)
	}
	if err := decoder.Verify(); err != nil {
		return nil, fmt.Errorf("decode article %s verify: %w", messageID, err)
	}
	return &decodedArticle{
		Offset:   decoder.PartOffset,
		FileSize: decoder.FileSize,
		Body:     body,
	}, nil
}

func fetchYencHeader(ctx context.Context, fetcher ArticleFetcher, groups []string, messageID string) (*nzb.YencHeader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := fetcher.Fetch(ctx, messageID, groups)
	if err != nil {
		return nil, fmt.Errorf("fetch article %s: %w", messageID, err)
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
	header, err := nzb.ReadYencHeader(reader)
	if err != nil {
		return nil, fmt.Errorf("decode article %s header: %w", messageID, err)
	}
	return &header, nil
}

func prepareArchiveProbeFiles(ctx context.Context, repo CatalogReader, fetcher ArticleFetcher, groups []string, family []pgindex.CatalogReleaseFile) ([]archiveProbeFile, error) {
	out := make([]archiveProbeFile, 0, len(family))
	for _, file := range family {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		refs, err := repo.ListCatalogReleaseFileArticles(ctx, file.ID)
		if err != nil {
			return nil, fmt.Errorf("list release file articles %d: %w", file.ID, err)
		}
		if len(refs) == 0 {
			return nil, fmt.Errorf("release file %d has no articles", file.ID)
		}
		header, err := fetchYencHeader(ctx, fetcher, groups, refs[0].MessageID)
		if err != nil {
			return nil, err
		}
		exactSize := file.SizeBytes
		if header != nil && header.FileSize > 0 {
			exactSize = header.FileSize
		}
		out = append(out, archiveProbeFile{
			file:      file,
			refs:      refs,
			exactSize: exactSize,
		})
	}
	return out, nil
}

func readArchiveRange(ctx context.Context, fetcher ArticleFetcher, groups []string, family []archiveProbeFile, start, end int64) ([]byte, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if end <= start {
		return nil, 0, fmt.Errorf("invalid archive range %d-%d", start, end)
	}

	length := end - start
	buffer := make([]byte, length)
	filled := int64(0)

	offset := int64(0)
	for _, file := range family {
		if err := ctx.Err(); err != nil {
			return buffer, filled, err
		}
		fileStart := offset
		fileEnd := fileStart + file.exactSize
		offset = fileEnd

		overlapStart := maxInt64(start, fileStart)
		overlapEnd := minInt64(end, fileEnd)
		if overlapEnd <= overlapStart {
			continue
		}

		written, err := materializeFileRange(ctx, fetcher, groups, file.refs, overlapStart-fileStart, overlapEnd-fileStart, buffer[overlapStart-start:overlapEnd-start])
		filled += written
		if err != nil {
			return buffer, filled, err
		}
	}

	return buffer, filled, nil
}

func materializeFileRange(ctx context.Context, fetcher ArticleFetcher, groups []string, refs []pgindex.CatalogArticleRef, fileRangeStart, fileRangeEnd int64, target []byte) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, fmt.Errorf("release file has no articles")
	}

	if fileRangeStart <= 0 && fileRangeEnd <= int64(len(target)) {
		return materializeFileRangeFromStart(ctx, fetcher, groups, refs, fileRangeEnd, target)
	}
	return materializeFileRangeFromEnd(ctx, fetcher, groups, refs, fileRangeStart, fileRangeEnd, target)
}

func materializeFileRangeFromStart(ctx context.Context, fetcher ArticleFetcher, groups []string, refs []pgindex.CatalogArticleRef, fileRangeEnd int64, target []byte) (int64, error) {
	const fileRangeStart int64 = 0
	var written int64
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		article, err := fetchDecodedArticle(ctx, fetcher, groups, ref.MessageID)
		if err != nil {
			return written, err
		}
		articleStart := article.Offset
		articleEnd := articleStart + int64(len(article.Body))
		overlapStart := maxInt64(fileRangeStart, articleStart)
		overlapEnd := minInt64(fileRangeEnd, articleEnd)
		if overlapEnd <= overlapStart {
			continue
		}

		srcStart := overlapStart - articleStart
		srcEnd := overlapEnd - articleStart
		dstStart := overlapStart - fileRangeStart
		dstEnd := overlapEnd - fileRangeStart
		copy(target[dstStart:dstEnd], article.Body[srcStart:srcEnd])
		written += overlapEnd - overlapStart
		if articleEnd >= fileRangeEnd {
			break
		}
	}
	return written, nil
}

func materializeFileRangeFromEnd(ctx context.Context, fetcher ArticleFetcher, groups []string, refs []pgindex.CatalogArticleRef, fileRangeStart, fileRangeEnd int64, target []byte) (int64, error) {
	var written int64
	for idx := len(refs) - 1; idx >= 0; idx-- {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		ref := refs[idx]
		article, err := fetchDecodedArticle(ctx, fetcher, groups, ref.MessageID)
		if err != nil {
			return written, err
		}
		articleStart := article.Offset
		articleEnd := articleStart + int64(len(article.Body))
		overlapStart := maxInt64(fileRangeStart, articleStart)
		overlapEnd := minInt64(fileRangeEnd, articleEnd)
		if overlapEnd <= overlapStart {
			if articleStart <= fileRangeStart {
				break
			}
			continue
		}

		srcStart := overlapStart - articleStart
		srcEnd := overlapEnd - articleStart
		dstStart := overlapStart - fileRangeStart
		dstEnd := overlapEnd - fileRangeStart
		copy(target[dstStart:dstEnd], article.Body[srcStart:srcEnd])
		written += overlapEnd - overlapStart
		if articleStart <= fileRangeStart {
			break
		}
	}
	return written, nil
}

func parseSevenZipNextHeaderRange(head []byte) (int64, int64, int64, uint32, error) {
	if len(head) < 32 {
		return 0, 0, 0, 0, fmt.Errorf("7z start header requires 32 bytes, got %d", len(head))
	}
	if !strings.EqualFold(fmt.Sprintf("%x", head[:6]), "377abcaf271c") {
		return 0, 0, 0, 0, fmt.Errorf("invalid 7z signature")
	}
	startHeaderCRC := binary.LittleEndian.Uint32(head[8:12])
	actualStartHeaderCRC := crc32.ChecksumIEEE(head[12:32])
	if actualStartHeaderCRC != startHeaderCRC {
		return 0, 0, 0, 0, fmt.Errorf("invalid 7z start header crc: expected %08X got %08X", startHeaderCRC, actualStartHeaderCRC)
	}

	nextOffset := int64(binary.LittleEndian.Uint64(head[12:20]))
	nextSize := int64(binary.LittleEndian.Uint64(head[20:28]))
	nextCRC := binary.LittleEndian.Uint32(head[28:32])
	if nextOffset < 0 || nextSize <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("invalid 7z next header range offset=%d size=%d", nextOffset, nextSize)
	}
	start := int64(32) + nextOffset
	end := start + nextSize
	return start, end, end, nextCRC, nil
}

func parseSevenZipEncodedHeaderPackRange(nextHeader []byte) (int64, int64, bool, error) {
	reader := sevenZipByteReader{data: nextHeader}
	nid, err := reader.readByte()
	if err != nil {
		return 0, 0, false, err
	}
	switch nid {
	case 0x01:
		return 0, 0, false, nil
	case 0x17:
	default:
		return 0, 0, false, nil
	}

	nid, err = reader.readByte()
	if err != nil {
		return 0, 0, true, fmt.Errorf("read encoded header streams info: %w", err)
	}
	if nid != 0x06 {
		return 0, 0, true, fmt.Errorf("unsupported 7z encoded header streams info nid 0x%02X", nid)
	}

	packPos, err := reader.readNumber()
	if err != nil {
		return 0, 0, true, fmt.Errorf("read encoded header pack pos: %w", err)
	}
	numPackStreams, err := reader.readNumber()
	if err != nil {
		return 0, 0, true, fmt.Errorf("read encoded header pack stream count: %w", err)
	}

	var packSizes []int64
	for {
		nid, err = reader.readByte()
		if err != nil {
			return 0, 0, true, fmt.Errorf("read encoded header pack property: %w", err)
		}
		switch nid {
		case 0x09:
			packSizes = make([]int64, 0, numPackStreams)
			for idx := int64(0); idx < numPackStreams; idx++ {
				size, err := reader.readNumber()
				if err != nil {
					return 0, 0, true, fmt.Errorf("read encoded header pack size: %w", err)
				}
				packSizes = append(packSizes, size)
			}
		case 0x0A:
			if err := reader.skipDigests(int(numPackStreams)); err != nil {
				return 0, 0, true, fmt.Errorf("skip encoded header digests: %w", err)
			}
		case 0x00:
			if len(packSizes) == 0 {
				return 0, 0, true, fmt.Errorf("encoded header pack sizes missing")
			}
			total := int64(0)
			for _, size := range packSizes {
				total += size
			}
			return packPos, packPos + total, true, nil
		default:
			return 0, 0, true, fmt.Errorf("unsupported 7z encoded header pack property nid 0x%02X", nid)
		}
	}
}

type sevenZipByteReader struct {
	data []byte
	pos  int
}

func (r *sevenZipByteReader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *sevenZipByteReader) readNumber() (int64, error) {
	first, err := r.readByte()
	if err != nil {
		return 0, err
	}

	mask := byte(0x80)
	value := int64(0)
	for i := 0; i < 8; i++ {
		if first&mask == 0 {
			high := int64(first & (mask - 1))
			value |= high << (8 * i)
			return value, nil
		}
		next, err := r.readByte()
		if err != nil {
			return 0, err
		}
		value |= int64(next) << (8 * i)
		mask >>= 1
	}
	return value, nil
}

func (r *sevenZipByteReader) skipDigests(count int) error {
	allDefined, err := r.readByte()
	if err != nil {
		return err
	}
	defined := make([]bool, count)
	if allDefined != 0 {
		for idx := range defined {
			defined[idx] = true
		}
	} else {
		mask := byte(0)
		var current byte
		for idx := range defined {
			if mask == 0 {
				current, err = r.readByte()
				if err != nil {
					return err
				}
				mask = 0x80
			}
			defined[idx] = current&mask != 0
			mask >>= 1
		}
	}
	for _, isDefined := range defined {
		if !isDefined {
			continue
		}
		if _, err := r.readN(4); err != nil {
			return err
		}
	}
	return nil
}

func (r *sevenZipByteReader) readN(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, io.EOF
	}
	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func parseSevenZipListing(output []byte, probePath string) ([]string, bool) {
	entries, encrypted := parseSevenZipListingDetails(output, probePath)
	return archiveEntryNames(entries), encrypted
}

func archiveEntryNames(entries []ArchiveEntryInfo) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		value := strings.TrimSpace(entry.Name)
		if value == "" {
			continue
		}
		names = append(names, value)
	}
	sort.Strings(names)
	return uniqueStrings(names)
}

func parseSevenZipListingDetails(output []byte, probePath string) ([]ArchiveEntryInfo, bool) {
	text := string(output)
	lower := strings.ToLower(text)
	encrypted := strings.Contains(lower, "encrypted = +") ||
		strings.Contains(lower, "enter password") ||
		strings.Contains(lower, "wrong password") ||
		strings.Contains(lower, "can not open encrypted archive")

	base := filepath.Base(probePath)
	entries := make([]ArchiveEntryInfo, 0, 8)
	current := ArchiveEntryInfo{}
	flush := func() {
		if strings.TrimSpace(current.Name) == "" {
			return
		}
		if filepath.Base(current.Name) == base {
			current = ArchiveEntryInfo{}
			return
		}
		entries = append(entries, current)
		current = ArchiveEntryInfo{}
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		parts := strings.SplitN(line, " = ", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "Path":
			flush()
			current.Name = value
		case "Folder":
			current.IsDir = value == "+"
		case "Size":
			current.UncompressedSize = ParseInt64(value)
		case "Packed Size":
			current.CompressedSize = ParseInt64(value)
		case "Encrypted":
			current.Encrypted = value == "+"
			encrypted = encrypted || current.Encrypted
		case "Comment":
			current.Comment = value
		}
	}
	flush()
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, encrypted
}

func MarshalArchiveEntries(entries []string) json.RawMessage {
	if len(entries) == 0 {
		return json.RawMessage(`[]`)
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return b
}
