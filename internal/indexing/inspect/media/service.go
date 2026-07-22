package media

import (
	"bytes"
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

const directMediaProbePrefixBytes int64 = 8 * 1024 * 1024

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
	inspectpkg.CatalogReader
}

type Service struct {
	repo      repository
	workspace *inspectpkg.WorkspaceManager
	fetcher   inspectpkg.ArticleFetcher
	runner    inspectpkg.CommandRunner
	log       logger
	opts      inspectpkg.Options
}

func NewService(repo repository, workspace *inspectpkg.WorkspaceManager, fetcher inspectpkg.ArticleFetcher, runner inspectpkg.CommandRunner, _ any, log logger, opts inspectpkg.Options) *Service {
	return &Service{repo: repo, workspace: workspace, fetcher: fetcher, runner: runner, log: log, opts: inspectpkg.DefaultOptions(opts)}
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
		Options: pgindex.BinaryInspectionCandidateOptions{
			RequireExpectedFileCount: s.opts.RequireExpectedFileCount,
		},
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
	if strings.TrimSpace(candidate.ReleaseID) == "" {
		if s != nil && s.log != nil {
			s.log.Warn("inspect_media: skipped candidate without release id binary_id=%d file=%s", candidate.BinaryID, candidate.FileName)
		}
		return s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:       stageName,
			BinaryID:        candidate.BinaryID,
			ReleaseID:       candidate.ReleaseID,
			SourceUpdatedAt: candidate.SourceUpdatedAt,
			Summary: map[string]any{
				"probe_skip_reason": "missing_release_id",
			},
		})
	}
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
	ffprobeWarning := ""
	prefixProbeWarning := ""
	archiveExtractError := ""
	materializedBytes := int64(0)
	mediaTitle := ""
	artifactRows := make([]pgindex.BinaryInspectionArtifactRecord, 0)
	streamRows := make([]pgindex.BinaryMediaStreamRecord, 0)

	strongArchiveHeuristics := archiveBacked && shouldSkipArchiveProbe(isVideo, isAudio, resolution, videoCodec, audioCodec)
	if strongArchiveHeuristics {
		probeMode = "heuristic_archive_entry"
	}

	if (isVideo || isAudio) && s.fetcher != nil && s.runner != nil && !archiveBacked {
		sampleCtx, sampleCancel := context.WithTimeout(ctx, s.opts.ToolTimeout)
		sample, err := inspectpkg.SampleBinaryPrefix(sampleCtx, s.repo, s.fetcher, candidate, directMediaProbePrefixLimit(s.opts.MaxBytes))
		sampleCancel()
		if err == nil {
			probeMode = "ffprobe_direct_prefix"
			materializedBytes += sample.BytesRead
			probeCtx, cancel := context.WithTimeout(ctx, s.opts.ToolTimeout)
			ffprobeResult, ffprobeOutput, probeErr := inspectpkg.RunFFProbeInput(probeCtx, s.runner, s.opts.FFProbePath, bytes.NewReader(sample.Prefix))
			cancel()
			artifactMetadata := map[string]any{
				"probe_mode":          "ffprobe_direct_prefix",
				"prefix_bytes":        sample.BytesRead,
				"exact_size":          sample.ExactSize,
				"streamed_to_ffprobe": true,
			}
			artifactRows = []pgindex.BinaryInspectionArtifactRecord{{
				BinaryID:     candidate.BinaryID,
				ReleaseID:    candidate.ReleaseID,
				StageName:    stageName,
				ArtifactRole: "decoded_media_prefix",
				ArtifactName: candidate.FileName,
				BytesTotal:   sample.BytesRead,
				MIMEType:     sample.MIMEType,
				Signature:    sample.Signature,
				SourceKind:   "inspect_media",
				Metadata:     artifactMetadata,
			}}
			if probeErr != nil {
				prefixProbeWarning = ffprobeDetail(ffprobeOutput, probeErr)
				if expectedTruncatedPrefixProbeEOF(sample.BytesRead, sample.ExactSize, prefixProbeWarning) {
					artifactMetadata["ffprobe_warning_detail"] = prefixProbeWarning
					if sample.ExactSize <= s.opts.MaxBytes {
						probeMode = "ffprobe_full_fallback"
						fullPath := filepath.Join(workspace.Dir, "media-full"+strings.ToLower(filepath.Ext(candidate.FileName)))
						materialized, materializeErr := inspectpkg.MaterializeBinaryToWorkspace(ctx, s.repo, s.fetcher, candidate, fullPath, s.opts.MaxBytes)
						if materializeErr != nil {
							ffprobeResult = nil
							ffprobeOutput = nil
							probeErr = fmt.Errorf("materialize full media fallback: %w", materializeErr)
						} else {
							materializedBytes += materialized.BytesWritten
							artifactRows = append(artifactRows, pgindex.BinaryInspectionArtifactRecord{
								BinaryID:     candidate.BinaryID,
								ReleaseID:    candidate.ReleaseID,
								StageName:    stageName,
								ArtifactRole: "materialized_media_full",
								ArtifactName: candidate.FileName,
								BytesTotal:   materialized.BytesWritten,
								MIMEType:     materialized.MIMEType,
								Signature:    materialized.Signature,
								SourceKind:   "inspect_media",
								Metadata: map[string]any{
									"probe_mode":      "ffprobe_full_fallback",
									"exact_size":      materialized.ExactSize,
									"fallback_reason": "prefix_inconclusive",
								},
							})
							fullProbeCtx, fullCancel := context.WithTimeout(ctx, s.opts.ToolTimeout)
							ffprobeResult, ffprobeOutput, probeErr = inspectpkg.RunFFProbe(fullProbeCtx, s.runner, s.opts.FFProbePath, materialized.OutputPath)
							fullCancel()
						}
					}
				}
			}
			if ffprobeResult != nil {
				mediaTitle = firstNonEmpty(mediaTitle, ffprobeFormatTitle(ffprobeResult.Format.Tags))
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
				probeDetail := ffprobeDetail(ffprobeOutput, probeErr)
				if probeMode == "ffprobe_direct_prefix" && expectedTruncatedPrefixProbeEOF(sample.BytesRead, sample.ExactSize, probeDetail) {
					ffprobeWarning = probeDetail
					artifactMetadata["ffprobe_warning_detail"] = probeDetail
				} else {
					ffprobeError = probeDetail
					artifactMetadata["ffprobe_error_detail"] = probeDetail
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
				mediaTitle = firstNonEmpty(mediaTitle, ffprobeFormatTitle(ffprobeResult.Format.Tags))
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
		"resolution":                    resolution,
		"video_codec":                   videoCodec,
		"audio_codec":                   audioCodec,
		"file_extension":                strings.ToLower(filepath.Ext(candidate.FileName)),
		"probe_mode":                    probeMode,
		"media_title_extractor_version": "v2",
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
	if ffprobeWarning != "" {
		summary["ffprobe_warning_detail"] = ffprobeWarning
		summary["probe_skip_reason"] = "prefix_inconclusive"
	}
	if prefixProbeWarning != "" && probeMode == "ffprobe_full_fallback" {
		summary["prefix_probe_warning_detail"] = prefixProbeWarning
	}
	if archiveExtractError != "" {
		summary["archive_extract_error_detail"] = archiveExtractError
		summary["probe_skip_reason"] = "archive_extract_failed"
	}
	if mediaEntry != "" {
		summary["archive_entry"] = mediaEntry
		summary["archive_entry_count"] = len(archiveEntries)
	}
	if mediaTitle != "" {
		summary["media_title"] = mediaTitle
		summary["media_title_source"] = "ffprobe_format_tag"
	}
	if !archiveBacked && ffprobeError != "" {
		if err := s.repo.FailBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
			StageName:         stageName,
			BinaryID:          candidate.BinaryID,
			ReleaseID:         candidate.ReleaseID,
			ErrorText:         ffprobeError,
			MaterializedBytes: materializedBytes,
			ToolProvenance:    inspectpkg.ToolProvenance(s.opts, stageName),
			Summary:           summary,
			SourceUpdatedAt:   candidate.SourceUpdatedAt,
		}); err != nil {
			return err
		}
		return nil
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

func normalizeMatch(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func directMediaProbePrefixLimit(maxBytes int64) int64 {
	if maxBytes > 0 && maxBytes < directMediaProbePrefixBytes {
		return maxBytes
	}
	return directMediaProbePrefixBytes
}

func expectedTruncatedPrefixProbeEOF(bytesRead, exactSize int64, detail string) bool {
	if bytesRead <= 0 || exactSize <= bytesRead {
		return false
	}
	detail = strings.ToLower(strings.TrimSpace(detail))
	return strings.Contains(detail, "file ended prematurely") || strings.Contains(detail, "end of file")
}

func ffprobeDetail(output []byte, err error) string {
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return detail
	}
	if err != nil {
		return strings.TrimSpace(err.Error())
	}
	return ""
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

func ffprobeFormatTitle(tags map[string]string) string {
	for key, value := range tags {
		if strings.EqualFold(strings.TrimSpace(key), "title") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
