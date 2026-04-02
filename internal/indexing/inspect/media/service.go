package media

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
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
	StartBinaryInspection(ctx context.Context, stageName string, binaryID int64, releaseID string, sourceUpdatedAt *time.Time) error
	CompleteBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	FailBinaryInspection(ctx context.Context, in pgindex.BinaryInspectionRecord) error
	ApplyReleaseInspectionUpdate(ctx context.Context, in pgindex.ReleaseInspectionUpdate) error
}

type Service struct {
	repo      repository
	workspace *inspectpkg.WorkspaceManager
	log       logger
	opts      inspectpkg.Options
}

func NewService(repo repository, workspace *inspectpkg.WorkspaceManager, log logger, opts inspectpkg.Options) *Service {
	return &Service{repo: repo, workspace: workspace, log: log, opts: inspectpkg.DefaultOptions(opts)}
}

func (s *Service) RunOnce(ctx context.Context) error {
	candidates, err := s.repo.ListBinaryInspectionCandidates(ctx, string(supervisor.StageInspectMedia), s.opts.CandidateBatchSize)
	if err != nil {
		return fmt.Errorf("list inspect_media candidates: %w", err)
	}
	if len(candidates) == 0 {
		if s != nil && s.log != nil {
			s.log.Debug("inspect_media: no media probe candidates available")
		}
		return nil
	}

	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.inspectCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
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

	text := strings.ToLower(strings.Join([]string{
		candidate.ReleaseTitle,
		candidate.SourceTitle,
		candidate.DeobfuscatedTitle,
		candidate.FileName,
		candidate.BinaryName,
	}, " "))

	resolution := normalizeMatch(resolutionRE.FindString(text))
	videoCodec := normalizeMatch(videoCodecRE.FindString(text))
	audioCodec := normalizeMatch(audioCodecRE.FindString(text))
	isVideo := inspectpkg.IsVideoFile(candidate.FileName)
	isAudio := inspectpkg.IsAudioFile(candidate.FileName)
	videoCount := 0
	audioCount := 0
	if isVideo {
		videoCount = 1
	}
	if isAudio {
		audioCount = 1
	}
	mediaQuality := 45.0
	if resolution == "1080p" {
		mediaQuality = 72
	} else if resolution == "2160p" {
		mediaQuality = 88
	} else if resolution == "720p" {
		mediaQuality = 60
	}

	summary := map[string]any{
		"resolution":     resolution,
		"video_codec":    videoCodec,
		"audio_codec":    audioCodec,
		"file_extension": strings.ToLower(filepath.Ext(candidate.FileName)),
		"workspace_path": workspace.ManifestPath,
	}
	if err := s.repo.CompleteBinaryInspection(ctx, pgindex.BinaryInspectionRecord{
		StageName:         stageName,
		BinaryID:          candidate.BinaryID,
		ReleaseID:         candidate.ReleaseID,
		Status:            "completed",
		MaterializedBytes: workspace.MaterializedBytes,
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

	return s.repo.ApplyReleaseInspectionUpdate(ctx, pgindex.ReleaseInspectionUpdate{
		ReleaseID:         candidate.ReleaseID,
		VideoCount:        &videoCount,
		AudioCount:        &audioCount,
		PrimaryResolution: resolution,
		PrimaryVideoCodec: videoCodec,
		PrimaryAudioCodec: audioCodec,
		MediaTags:         tags,
		MediaQualityScore: &mediaQuality,
		MediaQualityTier:  mediaTier(mediaQuality),
		MetadataUpdatedAt: ptrTime(time.Now().UTC()),
	})
}

func normalizeMatch(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
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
