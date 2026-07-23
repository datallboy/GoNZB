package inspect

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/at-wat/ebml-go"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestArchiveMediaPrefixLimitUsesLowerDefaultBudget(t *testing.T) {
	limit := archiveMediaPrefixLimit("movie.7z/feature.film.2026.mkv", 512*1024*1024, 0)
	if want := int64(64 * 1024 * 1024); limit != want {
		t.Fatalf("expected default prefix limit %d, got %d", want, limit)
	}
}

func TestArchiveMediaLimitsUseSmallerSampleBudget(t *testing.T) {
	prefix := archiveMediaPrefixLimit("Release/Sample/sample-video.mkv", 512*1024*1024, 0)
	if want := int64(24 * 1024 * 1024); prefix != want {
		t.Fatalf("expected sample prefix limit %d, got %d", want, prefix)
	}

	output := archiveMediaOutputLimit("Release/Sample/sample-video.mkv", 0)
	if want := int64(16 * 1024 * 1024); output != want {
		t.Fatalf("expected sample output limit %d, got %d", want, output)
	}
}

func TestArchiveMediaOutputLimitHonorsMaxBytesButKeepsMinimum(t *testing.T) {
	limit := archiveMediaOutputLimit("feature.film.2026.mkv", 4*1024*1024)
	if want := minArchiveMediaOutputBytes; limit != want {
		t.Fatalf("expected minimum output limit %d, got %d", want, limit)
	}
}

func TestMaterializeArchiveMediaRARUsesBoundedSparsePrefixAndParsesMatroska(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z is required for the archive extraction integration test")
	}

	entryName := "Feature.2026.mkv"
	mediaHeader := archiveMediaMatroskaHeader(t)
	mediaPayload := append(append([]byte(nil), mediaHeader...), bytes.Repeat([]byte{0x55}, 2*1024*1024)...)
	rarPayload := buildStoredRAR4(t, entryName, mediaPayload)
	const articleBytes = 64 * 1024
	refs, articles := archiveMediaArticles(rarPayload, "feature.rar", articleBytes)
	repo := archiveMediaTestRepository{
		files: []pgindex.CatalogReleaseFile{{
			ID:        41,
			BinaryID:  17,
			FileName:  "feature.rar",
			SizeBytes: int64(len(rarPayload)),
		}},
		refs: refs,
	}
	fetcher := &archiveMediaTestFetcher{articles: articles}
	const probeBudget = 256 * 1024

	result, err := MaterializeArchiveMediaToWorkspace(
		context.Background(),
		repo,
		fetcher,
		ExecCommandRunner{},
		pgindex.BinaryInspectionCandidate{BinaryID: 17, ReleaseID: "release-rar", FileName: "feature.rar"},
		entryName,
		t.TempDir(),
		Options{SevenZipPath: "7z", FFProbePath: "ffprobe", MaxBytes: probeBudget},
		nil,
	)
	if err != nil {
		t.Fatalf("probe bounded RAR media: %v", err)
	}
	if result.Signature != "matroska" || result.FFProbeResult == nil {
		t.Fatalf("expected Matroska metadata from RAR member, got %+v", result)
	}
	if result.ArchiveBytes != probeBudget || result.ArchiveBytes >= int64(len(rarPayload)) {
		t.Fatalf("expected bounded archive prefix %d smaller than archive %d, got %d", probeBudget, len(rarPayload), result.ArchiveBytes)
	}
	if len(result.FFProbeResult.Streams) != 2 || result.FFProbeResult.Streams[0].Width != 1920 || result.FFProbeResult.Streams[0].Height != 1080 {
		t.Fatalf("unexpected Matroska streams from RAR member: %+v", result.FFProbeResult.Streams)
	}
	if fetcher.uniqueBodiesFetched() >= len(refs) {
		t.Fatalf("expected a subset of archive articles to be fetched, fetched=%d total=%d", fetcher.uniqueBodiesFetched(), len(refs))
	}
}

func TestMaterializeArchiveMediaRARStreamsNonMatroskaPrefixToFFProbe(t *testing.T) {
	for _, tool := range []string{"7z", "ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required for the archive ffprobe integration test", tool)
		}
	}

	mp4Path := filepath.Join(t.TempDir(), "fixture.mp4")
	cmd := exec.Command(
		"ffmpeg",
		"-v", "error",
		"-f", "lavfi",
		"-i", "color=c=black:s=320x180:d=1",
		"-c:v", "mpeg4",
		"-movflags", "+faststart",
		"-y",
		mp4Path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create MP4 fixture: %v: %s", err, output)
	}
	mp4Payload, err := os.ReadFile(mp4Path)
	if err != nil {
		t.Fatalf("read MP4 fixture: %v", err)
	}

	entryName := "Feature.2026.mp4"
	rarPayload := buildStoredRAR4(t, entryName, mp4Payload)
	refs, articles := archiveMediaArticles(rarPayload, "feature.rar", 64*1024)
	repo := archiveMediaTestRepository{
		files: []pgindex.CatalogReleaseFile{{
			ID:        42,
			BinaryID:  18,
			FileName:  "feature.rar",
			SizeBytes: int64(len(rarPayload)),
		}},
		refs: refs,
	}

	result, err := MaterializeArchiveMediaToWorkspace(
		context.Background(),
		repo,
		&archiveMediaTestFetcher{articles: articles},
		ExecCommandRunner{},
		pgindex.BinaryInspectionCandidate{BinaryID: 18, ReleaseID: "release-rar-mp4", FileName: "feature.rar"},
		entryName,
		t.TempDir(),
		Options{SevenZipPath: "7z", FFProbePath: "ffprobe", MaxBytes: 256 * 1024},
		nil,
	)
	if err != nil {
		t.Fatalf("probe MP4 from bounded RAR: %v", err)
	}
	if result.Signature != "mp4" || result.FFProbeResult == nil {
		t.Fatalf("expected ffprobe metadata from MP4 RAR member, got %+v", result)
	}
	if len(result.FFProbeResult.Streams) != 1 || result.FFProbeResult.Streams[0].CodecType != "video" {
		t.Fatalf("unexpected MP4 streams from RAR member: %+v", result.FFProbeResult.Streams)
	}
	if result.FFProbeResult.Streams[0].Width != 320 || result.FFProbeResult.Streams[0].Height != 180 {
		t.Fatalf("unexpected MP4 dimensions: %+v", result.FFProbeResult.Streams[0])
	}
}

func TestMaterializeArchiveMediaZIPUsesBoundedHeadAndDirectoryTail(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z is required for the ZIP extraction integration test")
	}

	entryName := "Feature.2026.mkv"
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	mediaHeader := &zip.FileHeader{Name: entryName, Method: zip.Store}
	mediaEntry, err := zipWriter.CreateHeader(mediaHeader)
	if err != nil {
		t.Fatalf("create ZIP media entry: %v", err)
	}
	if _, err := mediaEntry.Write(append(archiveMediaMatroskaHeader(t), bytes.Repeat([]byte{0x55}, 512*1024)...)); err != nil {
		t.Fatalf("write ZIP media entry: %v", err)
	}
	paddingEntry, err := zipWriter.CreateHeader(&zip.FileHeader{Name: "padding.bin", Method: zip.Store})
	if err != nil {
		t.Fatalf("create ZIP padding entry: %v", err)
	}
	if _, err := paddingEntry.Write(bytes.Repeat([]byte{0x77}, 2*1024*1024)); err != nil {
		t.Fatalf("write ZIP padding entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close ZIP fixture: %v", err)
	}

	refs, articles := archiveMediaArticles(archive.Bytes(), "feature.zip", 64*1024)
	repo := archiveMediaTestRepository{
		files: []pgindex.CatalogReleaseFile{{
			ID:        43,
			BinaryID:  19,
			FileName:  "feature.zip",
			SizeBytes: int64(archive.Len()),
		}},
		refs: refs,
	}
	const probeBudget = 256 * 1024
	result, err := MaterializeArchiveMediaToWorkspace(
		context.Background(),
		repo,
		&archiveMediaTestFetcher{articles: articles},
		ExecCommandRunner{},
		pgindex.BinaryInspectionCandidate{BinaryID: 19, ReleaseID: "release-zip", FileName: "feature.zip"},
		entryName,
		t.TempDir(),
		Options{SevenZipPath: "7z", FFProbePath: "ffprobe", MaxBytes: probeBudget},
		nil,
	)
	if err != nil {
		t.Fatalf("probe bounded ZIP media: %v", err)
	}
	if result.Signature != "matroska" || result.FFProbeResult == nil {
		t.Fatalf("expected Matroska metadata from ZIP member, got %+v", result)
	}
	if result.ArchiveBytes != probeBudget || result.ArchiveBytes >= int64(archive.Len()) {
		t.Fatalf("expected bounded ZIP head/tail budget %d smaller than archive %d, got %d", probeBudget, archive.Len(), result.ArchiveBytes)
	}
}

func TestCreateRARVolumeAliasesNormalizesObfuscatedPartNames(t *testing.T) {
	workspace := t.TempDir()
	family := []pgindex.CatalogReleaseFile{
		{FileName: "alpha.part01.rar", FileIndex: 1},
		{FileName: "bravo.part02.rar", FileIndex: 2},
		{FileName: "charlie.part03.rar", FileIndex: 3},
	}
	for _, file := range family {
		path := filepath.Join(workspace, file.FileName)
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatalf("create sparse RAR fixture %s: %v", path, err)
		}
	}
	if err := createRARVolumeAliases(workspace, family); err != nil {
		t.Fatalf("create RAR volume aliases: %v", err)
	}
	for alias, target := range map[string]string{
		"alpha.part02.rar": "bravo.part02.rar",
		"alpha.part03.rar": "charlie.part03.rar",
	} {
		got, err := os.Readlink(filepath.Join(workspace, alias))
		if err != nil {
			t.Fatalf("read RAR volume alias %s: %v", alias, err)
		}
		if got != target {
			t.Fatalf("expected RAR alias %s to target %s, got %s", alias, target, got)
		}
	}
}

type archiveMediaTestRepository struct {
	files []pgindex.CatalogReleaseFile
	refs  []pgindex.CatalogArticleRef
}

func (r archiveMediaTestRepository) ListCatalogReleaseFiles(context.Context, string) ([]pgindex.CatalogReleaseFile, error) {
	return r.files, nil
}

func (r archiveMediaTestRepository) ListCatalogReleaseFileArticles(context.Context, int64) ([]pgindex.CatalogArticleRef, error) {
	return r.refs, nil
}

func (archiveMediaTestRepository) ListCatalogReleaseNewsgroups(context.Context, string) ([]string, error) {
	return []string{"alt.binaries.test"}, nil
}

type archiveMediaTestFetcher struct {
	articles map[string]string
	fetched  map[string]int
}

func (f *archiveMediaTestFetcher) Fetch(_ context.Context, messageID string, _ []string) (io.Reader, error) {
	payload, ok := f.articles[messageID]
	if !ok {
		return nil, fmt.Errorf("unknown article %s", messageID)
	}
	if f.fetched == nil {
		f.fetched = make(map[string]int)
	}
	f.fetched[messageID]++
	return strings.NewReader(payload), nil
}

func (f *archiveMediaTestFetcher) uniqueBodiesFetched() int {
	return len(f.fetched)
}

func archiveMediaArticles(payload []byte, fileName string, partBytes int) ([]pgindex.CatalogArticleRef, map[string]string) {
	totalParts := (len(payload) + partBytes - 1) / partBytes
	refs := make([]pgindex.CatalogArticleRef, 0, totalParts)
	articles := make(map[string]string, totalParts)
	for part := 0; part < totalParts; part++ {
		start := part * partBytes
		end := start + partBytes
		if end > len(payload) {
			end = len(payload)
		}
		body := payload[start:end]
		messageID := fmt.Sprintf("<rar-media-%d@test>", part+1)
		encoded := encodeArchiveMediaYEnc(body)
		articles[messageID] = fmt.Sprintf(
			"=ybegin part=%d total=%d line=128 size=%d name=%s\r\n=ypart begin=%d end=%d\r\n%s\r\n=yend size=%d pcrc32=%08x\r\n",
			part+1,
			totalParts,
			len(payload),
			fileName,
			start+1,
			end,
			encoded,
			len(body),
			crc32.ChecksumIEEE(body),
		)
		refs = append(refs, pgindex.CatalogArticleRef{MessageID: messageID, PartNumber: part + 1, Bytes: int64(len(body))})
	}
	return refs, articles
}

func encodeArchiveMediaYEnc(data []byte) string {
	out := make([]byte, 0, len(data))
	for _, b := range data {
		encoded := b + 42
		if encoded == 0 || encoded == '\n' || encoded == '\r' || encoded == '=' {
			out = append(out, '=')
			encoded += 64
		}
		out = append(out, encoded)
	}
	return string(out)
}

func buildStoredRAR4(t *testing.T, entryName string, payload []byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	archive.Write([]byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00})
	writeRAR4Header(t, &archive, 0x73, 0, []byte{0, 0, 0, 0, 0, 0})

	name := []byte(entryName)
	var fileFields bytes.Buffer
	mustWriteBinary(t, &fileFields, uint32(len(payload)))
	mustWriteBinary(t, &fileFields, uint32(len(payload)))
	fileFields.WriteByte(3)
	mustWriteBinary(t, &fileFields, crc32.ChecksumIEEE(payload))
	mustWriteBinary(t, &fileFields, uint32(0))
	fileFields.WriteByte(20)
	fileFields.WriteByte(0x30)
	mustWriteBinary(t, &fileFields, uint16(len(name)))
	mustWriteBinary(t, &fileFields, uint32(0x81a4))
	fileFields.Write(name)
	writeRAR4Header(t, &archive, 0x74, 0x8000, fileFields.Bytes())
	archive.Write(payload)
	writeRAR4Header(t, &archive, 0x7b, 0, nil)
	return archive.Bytes()
}

func writeRAR4Header(t *testing.T, dst *bytes.Buffer, headType byte, flags uint16, fields []byte) {
	t.Helper()
	var header bytes.Buffer
	header.WriteByte(headType)
	mustWriteBinary(t, &header, flags)
	mustWriteBinary(t, &header, uint16(7+len(fields)))
	header.Write(fields)
	mustWriteBinary(t, dst, uint16(crc32.ChecksumIEEE(header.Bytes())))
	dst.Write(header.Bytes())
}

func mustWriteBinary(t *testing.T, dst io.Writer, value any) {
	t.Helper()
	if err := binary.Write(dst, binary.LittleEndian, value); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}
}

func archiveMediaMatroskaHeader(t *testing.T) []byte {
	t.Helper()
	document := matroskaHeaderDocument{
		EBML: matroskaEBMLHeader{DocType: "matroska"},
		Segment: matroskaHeaderSegment{
			Info: matroskaHeaderInfo{TimecodeScale: 1_000_000, Duration: 90_000, Title: "RAR Feature"},
			Tracks: matroskaHeaderTracks{Entries: []matroskaHeaderTrack{
				{
					TrackNumber: 1,
					TrackType:   1,
					CodecID:     "V_MPEG4/ISO/AVC",
					Video:       &matroskaHeaderVideo{PixelWidth: 1920, PixelHeight: 1080},
				},
				{
					TrackNumber: 2,
					TrackType:   2,
					CodecID:     "A_AAC",
					Language:    "eng",
					Audio:       &matroskaHeaderAudio{SamplingFrequency: 48000, Channels: 2},
				},
			}},
		},
	}
	var payload bytes.Buffer
	if err := ebml.Marshal(&document, &payload); err != nil {
		t.Fatalf("marshal Matroska fixture: %v", err)
	}
	return payload.Bytes()
}
