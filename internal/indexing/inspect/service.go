package inspect

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

var passwordHintRE = regexp.MustCompile(`(?i)(?:pass(?:word)?|pw)[\s:_-]*([a-z0-9._-]{3,})`)

type logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type Runner interface {
	RunOnce(ctx context.Context) error
}

type Service struct {
	log     logger
	runners []namedRunner
}

type namedRunner struct {
	name   string
	runner Runner
}

type Options struct {
	WorkDir            string
	WorkspaceBackend   string
	MemoryWorkDir      string
	MaxBytes           int64
	MaxArchiveDepth    int
	ToolTimeout        time.Duration
	FFProbePath        string
	SevenZipPath       string
	UnrarPath          string
	PAR2Path           string
	CandidateBatchSize int
	Concurrency        int
	ClaimOwner         string
	ClaimLease         time.Duration
}

func NewService(log logger, runners map[string]Runner) *Service {
	ordered := []string{
		string(supervisor.StageInspectDiscovery),
		string(supervisor.StageInspectPAR2),
		string(supervisor.StageInspectNFO),
		string(supervisor.StageInspectArchive),
		string(supervisor.StageInspectPassword),
		string(supervisor.StageInspectMedia),
	}
	items := make([]namedRunner, 0, len(ordered))
	for _, name := range ordered {
		runner := runners[name]
		if runner == nil {
			continue
		}
		items = append(items, namedRunner{name: name, runner: runner})
	}
	return &Service{log: log, runners: items}
}

func (s *Service) RunOnce(ctx context.Context) error {
	for _, runner := range s.runners {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := runner.runner.RunOnce(ctx); err != nil {
			return fmt.Errorf("%s: %w", runner.name, err)
		}
	}
	if s != nil && s.log != nil {
		s.log.Info("inspect: ran submodules=%d", len(s.runners))
	}
	return nil
}

func DefaultOptions(opts Options) Options {
	if strings.TrimSpace(opts.WorkDir) == "" {
		opts.WorkDir = "/store/indexer/inspect"
	}
	opts.WorkspaceBackend = strings.ToLower(strings.TrimSpace(opts.WorkspaceBackend))
	if opts.WorkspaceBackend == "" {
		opts.WorkspaceBackend = "auto"
	}
	if opts.WorkspaceBackend != "disk" && opts.WorkspaceBackend != "memory" && opts.WorkspaceBackend != "auto" {
		opts.WorkspaceBackend = "auto"
	}
	if strings.TrimSpace(opts.MemoryWorkDir) == "" {
		opts.MemoryWorkDir = "/dev/shm/gonzb-inspect"
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 2 * 1024 * 1024 * 1024
	}
	if opts.MaxArchiveDepth <= 0 {
		opts.MaxArchiveDepth = 3
	}
	if opts.ToolTimeout <= 0 {
		opts.ToolTimeout = 30 * time.Second
	}
	if strings.TrimSpace(opts.FFProbePath) == "" {
		opts.FFProbePath = "ffprobe"
	}
	if strings.TrimSpace(opts.SevenZipPath) == "" {
		opts.SevenZipPath = "7z"
	}
	if strings.TrimSpace(opts.UnrarPath) == "" {
		opts.UnrarPath = "unrar"
	}
	if strings.TrimSpace(opts.PAR2Path) == "" {
		opts.PAR2Path = "par2"
	}
	if opts.CandidateBatchSize <= 0 {
		opts.CandidateBatchSize = 100
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if strings.TrimSpace(opts.ClaimOwner) == "" {
		opts.ClaimOwner = "inspect"
	}
	if opts.ClaimLease <= 0 {
		opts.ClaimLease = 15 * time.Minute
	}
	return opts
}

func ToolProvenance(opts Options, stageName string) map[string]any {
	base := map[string]any{
		"stage":            stageName,
		"tool_timeout_sec": int(opts.ToolTimeout / time.Second),
	}
	switch stageName {
	case string(supervisor.StageInspectPAR2):
		base["tool"] = opts.PAR2Path
	case string(supervisor.StageInspectArchive), string(supervisor.StageInspectPassword):
		base["tool"] = opts.SevenZipPath
		base["fallback_tool"] = opts.UnrarPath
	case string(supervisor.StageInspectMedia):
		base["tool"] = opts.FFProbePath
	default:
		base["tool"] = "heuristic"
	}
	return base
}

func ExtractPasswordCandidates(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		matches := passwordHintRE.FindAllStringSubmatch(strings.ToLower(value), -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			candidate := strings.TrimSpace(match[1])
			if candidate == "" {
				continue
			}
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func IsVideoFile(fileName string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".mkv", ".mp4", ".avi", ".ts":
		return true
	default:
		return false
	}
}

func IsAudioFile(fileName string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".flac", ".mp3", ".m4a":
		return true
	default:
		return false
	}
}

func InferEncrypted(candidate pgindex.BinaryInspectionCandidate) bool {
	text := strings.ToLower(strings.Join([]string{
		candidate.ReleaseTitle,
		candidate.SourceTitle,
		candidate.DeobfuscatedTitle,
		candidate.FileName,
		candidate.BinaryName,
	}, " "))
	return strings.Contains(text, "password") ||
		strings.Contains(text, "protected") ||
		strings.Contains(text, "encrypted")
}
