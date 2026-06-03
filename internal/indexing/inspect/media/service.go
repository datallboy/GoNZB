package media

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

var (
	resolutionRE = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p)\b`)
	videoCodecRE = regexp.MustCompile(`(?i)\b(x265|h265|hevc|av1|x264|h264|xvid)\b`)
	audioCodecRE = regexp.MustCompile(`(?i)\b(truehd|atmos|dts[- ]?hd|dts|ddp|eac3|ac3|aac|flac|mp3)\b`)
)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type repository interface {
	ListBinaryInspectionCandidates(ctx context.Context, stageName string, limit int) ([]pgindex.BinaryInspectionCandidate, error)
	ClaimBinaryInspectionCandidates(ctx context.Context, req pgindex.BinaryInspectionClaimRequest) ([]pgindex.BinaryInspectionCandidate, error)
	StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error
	CompleteBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	FailBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	ReplaceBinaryInspectionArtifacts(ctx context.Context, stageName string, binaryID int64, rows []pgindex.BinaryInspectionArtifactRecord) error
	ReplaceBinaryMediaStreams(ctx context.Context, binaryID int64, rows []pgindex.BinaryMediaStreamRecord) error
	ApplyReleaseInspectionUpdate(ctx context.Context, in pgindex.ReleaseInspectionUpdate) error
	SetReleaseArchivePreview(ctx context.Context, releaseID, objectKey, contentType, sourceKind string) error
	inspectpkg.CatalogReader
}

type blobStore interface {
	SaveObjectAtomically(key string, data []byte) error
}

type Service struct {
	repo      repository
	workspace *inspectpkg.WorkspaceManager
	fetcher   inspectpkg.ArticleFetcher
	runner    inspectpkg.CommandRunner
	store     blobStore
	log       logger
	opts      inspectpkg.Options
}

func NewService(repo repository, workspace *inspectpkg.WorkspaceManager, fetcher inspectpkg.ArticleFetcher, runner inspectpkg.CommandRunner, store blobStore, log logger, opts inspectpkg.Options) *Service {
	return &Service{repo: repo, workspace: workspace, fetcher: fetcher, runner: runner, store: store, log: log, opts: inspectpkg.DefaultOptions(opts)}
}

func (s *Service) RunOnce(ctx context.Context) error {
	_, err := s.RunOnceWithMetrics(ctx)
	return err
}

func (s *Service) RunOnceWithMetrics(ctx context.Context) (map[string]any, error) {
	stageName := string(supervisor.StageInspectMedia)
	candidates, err := s.repo.ClaimBinaryInspectionCandidates(ctx, pgindex.BinaryInspectionClaimRequest{
		StageName:     stageName,
		Limit:         s.opts.CandidateBatchSize,
		Owner:         s.opts.ClaimOwner + ":" + stageName,
		LeaseDuration: s.opts.ClaimLease,
	})
	if err != nil {
		return nil, fmt.Errorf("claim inspect_media candidates: %w", err)
	}
	metrics := map[string]any{
		"candidate_count": len(candidates),
		"reserved_count":  len(candidates),
		"processed_count": 0,
		"failed_count":    0,
		"batch_size":      s.opts.CandidateBatchSize,
		"worker_count":    s.opts.Concurrency,
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_media: no media probe candidates available")
		}
		return metrics, nil
	}

	processed, failed, err := s.processCandidates(ctx, candidates)
	metrics["processed_count"] = processed
	metrics["failed_count"] = failed
	return metrics, err
}

func (s *Service) processCandidates(ctx context.Context, candidates []pgindex.BinaryInspectionCandidate) (int, int, error) {
	processed := 0
	failed := 0
	if s.opts.Concurrency <= 1 || len(candidates) <= 1 {
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return processed, failed, err
			}
			if err := s.inspectCandidate(ctx, candidate); err != nil {
				failed++
				return processed, failed, err
			}
			processed++
		}
		return processed, failed, nil
	}

	workerCount := s.opts.Concurrency
	if workerCount > len(candidates) {
		workerCount = len(candidates)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan pgindex.BinaryInspectionCandidate)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if err := workerCtx.Err(); err != nil {
					recordErr(err)
					return
				}
				if err := s.inspectCandidate(workerCtx, candidate); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					recordErr(err)
					return
				}
				mu.Lock()
				processed++
				mu.Unlock()
			}
		}()
	}

	for _, candidate := range candidates {
		if err := workerCtx.Err(); err != nil {
			recordErr(err)
			break
		}
		select {
		case jobs <- candidate:
		case <-workerCtx.Done():
			recordErr(workerCtx.Err())
		}
	}
	close(jobs)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	return processed, failed, firstErr
}

func (s *Service) inspectCandidate(ctx context.Context, candidate pgindex.BinaryInspectionCandidate) error {
	stageName := string(supervisor.StageInspectMedia)
	if err := s.repo.StartBinaryInspection(ctx, stageName, candidate.BinaryID, candidate.ReleaseID, candidate.SourceUpdatedAt); err != nil {
		return err
	}

	workspace, err := s.workspace.PrepareBinaryWorkspace(ctx, stageName, candidate)
	if err != nil {
		_ = s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			ErrorText:       err.Error(),
			SourceUpdatedAt: candidate.SourceUpdatedAt,
		})
		return fmt.Errorf("prepare media workspace: %w", err)
	}
	defer workspace.Cleanup()

	stagePath := filepath.Join(workspace.Dir, filepath.Base(candidate.FileName))
	text := strings.ToLower(strings.Join([]string{
		candidate.ReleaseTitle,
		candidate.SourceTitle,
		candidate.DeobfuscatedTitle,
		candidate.FileName,
		candidate.BinaryName,
	}, " "))
	archiveEntries := inspectpkg.ArchiveEntryNamesFromSummary(candidate.ArchiveSummaryJSON)
	mediaEntry := inspectpkg.BestMediaEntry(archiveEntries)
	if mediaEntry != "" {
		text = strings.ToLower(strings.Join([]string{text, mediaEntry, strings.Join(archiveEntries, " ")}, " "))
	}

	resolution := normalizeMatch(resolutionRE.FindString(text))
	videoCodec := normalizeMatch(videoCodecRE.FindString(text))
	audioCodec := normalizeMatch(audioCodecRE.FindString(text))
	subtitles := make([]string, 0, 2)
	isVideo := inspectpkg.IsVideoFile(candidate.FileName)
	isAudio := inspectpkg.IsAudioFile(candidate.FileName)
	archiveBacked := inspectpkg.IsArchiveFile(candidate.FileName) && mediaEntry != ""
	if mediaEntry != "" {
		isVideo = isVideo || inspectpkg.IsVideoFile(mediaEntry)
		isAudio = isAudio || inspectpkg.IsAudioFile(mediaEntry)
	}
	videoCount := 0
	audioCount := 0
	runtimeSeconds := 0
	mediaQuality := 45.0
	if resolution == "1080p" {
		mediaQuality = 72
	} else if resolution == "2160p" {
		mediaQuality = 88
	} else if resolution == "720p" {
		mediaQuality = 60
	}
	probeMode := "heuristic"
	ffprobeError := ""
	archiveExtractError := ""
	materializedBytes := workspace.MaterializedBytes
	artifactRows := make([]pgindex.BinaryInspectionArtifactRecord, 0)
	streamRows := make([]pgindex.BinaryMediaStreamRecord, 0)

	strongArchiveHeuristics := archiveBacked && shouldSkipArchiveProbe(isVideo, isAudio, resolution, videoCodec, audioCodec)
	if strongArchiveHeuristics {
		probeMode = "heuristic_archive_entry"
	}

	if (isVideo || isAudio) && s.fetcher != nil && s.runner != nil && !archiveBacked {
		materialized, err := inspectpkg.MaterializeBinaryToWorkspace(ctx, s.repo, s.fetcher, candidate, stagePath, s.opts.MaxBytes)
		if err == nil {
			probeMode = "ffprobe_direct"
			materializedBytes += materialized.BytesWritten
			probeCtx, cancel := context.WithTimeout(ctx, s.opts.ToolTimeout)
			ffprobeResult, ffprobeOutput, probeErr := inspectpkg.RunFFProbe(probeCtx, s.runner, s.opts.FFProbePath, materialized.OutputPath)
			cancel()
			artifactRows = []pgindex.BinaryInspectionArtifactRecord{{
				BinaryID:     candidate.BinaryID,
				ReleaseID:    candidate.ReleaseID,
				StageName:    stageName,
				ArtifactRole: "decoded_file",
				ArtifactName: candidate.FileName,
				BytesTotal:   materialized.ExactSize,
				MIMEType:     materialized.MIMEType,
				Signature:    materialized.Signature,
				SourceKind:   "inspect_media",
				Metadata: map[string]any{
					"probe_mode":           "ffprobe_direct",
					"ffprobe_error_detail": errorString(probeErr),
				},
			}}
			if ffprobeResult != nil {
				for _, stream := range ffprobeResult.Streams {
					language := ""
					if stream.Tags != nil {
						language = normalizeMatch(stream.Tags["language"])
					}
					if stream.CodecType == "video" {
						videoCount++
						if stream.Height > 0 && resolution == "" {
							resolution = fmt.Sprintf("%dp", stream.Height)
						}
						if stream.CodecName != "" && videoCodec == "" {
							videoCodec = normalizeMatch(stream.CodecName)
						}
					}
					if stream.CodecType == "audio" {
						audioCount++
						if stream.CodecName != "" && audioCodec == "" {
							audioCodec = normalizeMatch(stream.CodecName)
						}
					}
					if stream.CodecType == "subtitle" && language != "" {
						subtitles = append(subtitles, language)
					}
					streamRows = append(streamRows, pgindex.BinaryMediaStreamRecord{
						BinaryID:           candidate.BinaryID,
						ReleaseID:          candidate.ReleaseID,
						StreamIndex:        stream.Index,
						StreamType:         stream.CodecType,
						CodecName:          stream.CodecName,
						CodecLongName:      stream.CodecLong,
						Profile:            stream.Profile,
						Width:              stream.Width,
						Height:             stream.Height,
						Channels:           stream.Channels,
						Language:           language,
						DurationSeconds:    inspectpkg.ParseSeconds(firstNonEmpty(stream.Duration, ffprobeResult.Format.Duration)),
						BitRate:            inspectpkg.ParseInt64(firstNonEmpty(stream.BitRate, ffprobeResult.Format.BitRate)),
						DefaultDisposition: stream.Disposition.Default == 1,
						ForcedDisposition:  stream.Disposition.Forced == 1,
						Metadata: map[string]any{
							"format_name":          ffprobeResult.Format.FormatName,
							"format_long_name":     ffprobeResult.Format.FormatLongName,
							"format_probe_score":   ffprobeResult.Format.ProbeScore,
							"format_tags":          ffprobeResult.Format.Tags,
							"codec_tag_string":     stream.CodecTagString,
							"codec_tag":            stream.CodecTag,
							"sample_rate":          stream.SampleRate,
							"channel_layout":       stream.ChannelLayout,
							"sample_format":        stream.SampleFormat,
							"bits_per_sample":      stream.BitsPerSample,
							"pix_fmt":              stream.PixFmt,
							"display_aspect_ratio": stream.DisplayAspectRatio,
							"r_frame_rate":         stream.RFrameRate,
							"avg_frame_rate":       stream.AvgFrameRate,
							"stream_tags":          stream.Tags,
						},
					})
				}
				runtimeSeconds = int(inspectpkg.ParseSeconds(ffprobeResult.Format.Duration))
			}
			if probeErr != nil {
				ffprobeError = strings.TrimSpace(string(ffprobeOutput))
				if ffprobeError == "" && probeErr != nil {
					ffprobeError = probeErr.Error()
				}
			}
		} else {
			ffprobeError = err.Error()
		}
	}
	if (isVideo || isAudio) && archiveBacked && s.fetcher != nil && !strongArchiveHeuristics {
		archiveMedia, err := inspectpkg.MaterializeArchiveMediaToWorkspace(ctx, s.repo, s.fetcher, s.runner, candidate, mediaEntry, workspace.Dir, s.opts, s.log)
		if err == nil {
			probeMode = "ffprobe_archive"
			materializedBytes += archiveMedia.ArchiveBytes + archiveMedia.ExtractedBytes
			artifactRows = []pgindex.BinaryInspectionArtifactRecord{{
				BinaryID:     candidate.BinaryID,
				ReleaseID:    candidate.ReleaseID,
				StageName:    stageName,
				ArtifactRole: "archive_member_prefix",
				ArtifactName: mediaEntry,
				BytesTotal:   archiveMedia.ExtractedBytes,
				MIMEType:     archiveMedia.MIMEType,
				Signature:    archiveMedia.Signature,
				SourceKind:   "inspect_media",
				Metadata: map[string]any{
					"probe_mode":          "ffprobe_archive",
					"archive_entry":       mediaEntry,
					"extract_stderr":      archiveMedia.ExtractStderr,
					"partial_extraction":  archiveMedia.PartialExtraction,
					"streamed_to_ffprobe": true,
				},
			}}

			ffprobeResult, ffprobeOutput, probeErr := archiveMedia.FFProbeResult, archiveMedia.FFProbeOutput, error(nil)
			if archiveMedia.FFProbeError != "" {
				probeErr = fmt.Errorf("%s", archiveMedia.FFProbeError)
			} else if ffprobeResult == nil && len(ffprobeOutput) == 0 {
				probeErr = fmt.Errorf("ffprobe archive probe returned no result")
			}
			if ffprobeResult != nil {
				for _, stream := range ffprobeResult.Streams {
					language := ""
					if stream.Tags != nil {
						language = normalizeMatch(stream.Tags["language"])
					}
					if stream.CodecType == "video" {
						videoCount++
						if stream.Height > 0 && resolution == "" {
							resolution = fmt.Sprintf("%dp", stream.Height)
						}
						if stream.CodecName != "" && videoCodec == "" {
							videoCodec = normalizeMatch(stream.CodecName)
						}
					}
					if stream.CodecType == "audio" {
						audioCount++
						if stream.CodecName != "" && audioCodec == "" {
							audioCodec = normalizeMatch(stream.CodecName)
						}
					}
					if stream.CodecType == "subtitle" && language != "" {
						subtitles = append(subtitles, language)
					}
					streamRows = append(streamRows, pgindex.BinaryMediaStreamRecord{
						BinaryID:           candidate.BinaryID,
						ReleaseID:          candidate.ReleaseID,
						StreamIndex:        stream.Index,
						StreamType:         stream.CodecType,
						CodecName:          stream.CodecName,
						CodecLongName:      stream.CodecLong,
						Profile:            stream.Profile,
						Width:              stream.Width,
						Height:             stream.Height,
						Channels:           stream.Channels,
						Language:           language,
						DurationSeconds:    inspectpkg.ParseSeconds(firstNonEmpty(stream.Duration, ffprobeResult.Format.Duration)),
						BitRate:            inspectpkg.ParseInt64(firstNonEmpty(stream.BitRate, ffprobeResult.Format.BitRate)),
						DefaultDisposition: stream.Disposition.Default == 1,
						ForcedDisposition:  stream.Disposition.Forced == 1,
						Metadata: map[string]any{
							"format_name":          ffprobeResult.Format.FormatName,
							"format_long_name":     ffprobeResult.Format.FormatLongName,
							"format_probe_score":   ffprobeResult.Format.ProbeScore,
							"format_tags":          ffprobeResult.Format.Tags,
							"codec_tag_string":     stream.CodecTagString,
							"codec_tag":            stream.CodecTag,
							"sample_rate":          stream.SampleRate,
							"channel_layout":       stream.ChannelLayout,
							"sample_format":        stream.SampleFormat,
							"bits_per_sample":      stream.BitsPerSample,
							"pix_fmt":              stream.PixFmt,
							"display_aspect_ratio": stream.DisplayAspectRatio,
							"r_frame_rate":         stream.RFrameRate,
							"avg_frame_rate":       stream.AvgFrameRate,
							"stream_tags":          stream.Tags,
							"archive_entry":        mediaEntry,
						},
					})
				}
				runtimeSeconds = int(inspectpkg.ParseSeconds(ffprobeResult.Format.Duration))
			}
			if probeErr != nil {
				ffprobeError = strings.TrimSpace(string(ffprobeOutput))
				if ffprobeError == "" {
					ffprobeError = probeErr.Error()
				}
			}
		} else {
			archiveExtractError = err.Error()
		}
	}
	if videoCount == 0 && isVideo {
		videoCount = 1
	}
	if audioCount == 0 && isAudio {
		audioCount = 1
	}
	if err := s.repo.ReplaceBinaryInspectionArtifacts(ctx, stageName, candidate.BinaryID, artifactRows); err != nil {
		return err
	}
	if err := s.repo.ReplaceBinaryMediaStreams(ctx, candidate.BinaryID, streamRows); err != nil {
		return err
	}

	summary := map[string]any{
		"resolution":     resolution,
		"video_codec":    videoCodec,
		"audio_codec":    audioCodec,
		"file_extension": strings.ToLower(filepath.Ext(candidate.FileName)),
		"probe_mode":     probeMode,
	}
	if runtimeSeconds > 0 {
		summary["runtime_seconds"] = runtimeSeconds
	}
	if len(subtitles) > 0 {
		summary["subtitle_languages"] = subtitles
	}
	if ffprobeError != "" {
		summary["ffprobe_error_detail"] = ffprobeError
		summary["probe_skip_reason"] = "ffprobe_failed"
	}
	if archiveExtractError != "" {
		summary["archive_extract_error_detail"] = archiveExtractError
		summary["probe_skip_reason"] = "archive_extract_failed"
	}
	if mediaEntry != "" {
		summary["archive_entry"] = mediaEntry
		summary["archive_entry_count"] = len(archiveEntries)
	}
	if err := s.persistArchivePreview(ctx, candidate, archiveEntries, mediaEntry, stagePath, archiveBacked, probeMode, summary); err != nil && s.log != nil {
		s.log.Warn("inspect_media: preview generation skipped binary_id=%d release_id=%s err=%v", candidate.BinaryID, candidate.ReleaseID, err)
	}
	if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: materializedBytes,
		ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
		Summary:           summary,
		SourceUpdatedAt:   candidate.SourceUpdatedAt,
	}); err != nil {
		return err
	}

	tags := make([]string, 0, 3)
	if resolution != "" {
		tags = append(tags, resolution)
	}
	if videoCodec != "" {
		tags = append(tags, videoCodec)
	}
	if audioCodec != "" {
		tags = append(tags, audioCodec)
	}

	if err := s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		VideoCount:        &videoCount,
		AudioCount:        &audioCount,
		RuntimeSeconds:    ptrInt(runtimeSeconds),
		PrimaryResolution: resolution,
		PrimaryVideoCodec: videoCodec,
		PrimaryAudioCodec: audioCodec,
		SubtitleLanguages: subtitles,
		MediaTags:         tags,
		MediaQualityScore: &mediaQuality,
		MediaQualityTier:  mediaTier(mediaQuality),
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	}); err != nil {
		if pgindex.IsReleaseNotFound(err) {
			if s != nil && s.log != nil {
				s.log.Warn("inspect_media: skipped stale release rollup binary_id=%d release_id=%s", candidate.BinaryID, candidate.ReleaseID)
			}
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) persistArchivePreview(ctx context.Context, candidate pgindex.BinaryInspectionCandidate, archiveEntries []string, mediaEntry, stagePath string, archiveBacked bool, probeMode string, summary map[string]any) error {
	if s.store == nil || s.repo == nil {
		return nil
	}
	previewBytes, contentType, sourceKind, err := s.generatePreview(ctx, candidate, archiveEntries, mediaEntry, stagePath, archiveBacked)
	if err != nil {
		return err
	}
	if len(previewBytes) == 0 {
		return nil
	}
	objectKey := inspectpkg.PreviewObjectKey(candidate.ProviderID, candidate.ReleaseID, contentType)
	if err := s.store.SaveObjectAtomically(objectKey, previewBytes); err != nil {
		return err
	}
	if err := s.repo.SetReleaseArchivePreview(ctx, candidate.ReleaseID, objectKey, contentType, sourceKind); err != nil {
		return err
	}
	summary["preview_object_key"] = objectKey
	summary["preview_source_kind"] = sourceKind
	summary["preview_content_type"] = contentType
	summary["preview_probe_mode"] = probeMode
	return nil
}

func (s *Service) generatePreview(ctx context.Context, candidate pgindex.BinaryInspectionCandidate, archiveEntries []string, mediaEntry, stagePath string, archiveBacked bool) ([]byte, string, string, error) {
	imageEntry := inspectpkg.BestPreviewImageEntry(archiveEntries)
	if imageEntry != "" && s.fetcher != nil && s.runner != nil {
		workspaceDir := filepath.Dir(stagePath)
		data, mimeType, err := inspectpkg.MaterializeArchivePreviewImage(ctx, s.repo, s.fetcher, s.runner, candidate, imageEntry, workspaceDir, s.opts, s.log)
		if err == nil && len(data) > 0 {
			if archiveBacked {
				return data, mimeType, "archive_image", nil
			}
			return data, mimeType, "archive_image_prearchive", nil
		}
	}
	if strings.TrimSpace(s.opts.FFmpegPath) == "" || s.runner == nil {
		return nil, "", "", nil
	}
	if mediaEntry != "" && s.fetcher != nil && inspectpkg.IsVideoFile(mediaEntry) {
		workspaceDir := filepath.Dir(stagePath)
		data, err := inspectpkg.MaterializeArchiveVideoThumbnail(ctx, s.repo, s.fetcher, s.runner, candidate, mediaEntry, workspaceDir, s.opts, s.log)
		if err == nil && len(data) > 0 {
			if archiveBacked {
				return data, "image/jpeg", "ffmpeg_archive", nil
			}
			return data, "image/jpeg", "ffmpeg_archive_prearchive", nil
		}
	}
	if !archiveBacked && inspectpkg.IsVideoFile(candidate.FileName) {
		data, err := inspectpkg.RunFFmpegThumbnail(ctx, s.runner, s.opts.FFmpegPath, stagePath)
		if err == nil && len(data) > 0 {
			return data, "image/jpeg", "ffmpeg_direct", nil
		}
	}
	return nil, "", "", nil
}

func normalizeMatch(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func shouldSkipArchiveProbe(isVideo, isAudio bool, resolution, videoCodec, audioCodec string) bool {
	if isAudio && audioCodec != "" {
		return true
	}
	if isVideo && resolution != "" && (videoCodec != "" || audioCodec != "") {
		return true
	}
	return false
}

func mediaTier(score float64) string {
	switch {
	case score >= 85:
		return "premium"
	case score >= 65:
		return "good"
	default:
		return "standard"
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

func ptrInt(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
