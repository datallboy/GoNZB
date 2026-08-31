package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	workerconfig "github.com/datallboy/gonzb/internal/worker/config"
	"github.com/datallboy/gonzb/internal/worker/ingest"
	"github.com/datallboy/gonzb/internal/worker/jobs"
	"github.com/datallboy/gonzb/internal/worker/naming"
	"github.com/datallboy/gonzb/internal/worker/pesto"
	"github.com/datallboy/gonzb/internal/worker/qbittorrent"
	"github.com/datallboy/gonzb/internal/worker/transfer"
	"github.com/segmentio/ksuid"
)

var torrentHashPattern = regexp.MustCompile(`(?i)^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

type Runner struct {
	config       *workerconfig.Config
	store        *jobs.Store
	qbit         *qbittorrent.Client
	transfer     *transfer.Client
	pesto        *pesto.Client
	ingest       *ingest.Client
	logger       *slog.Logger
	pestoVersion string
}

func New(config *workerconfig.Config, store *jobs.Store, qbit *qbittorrent.Client, transferClient *transfer.Client, pestoClient *pesto.Client, ingestClient *ingest.Client, logger *slog.Logger) *Runner {
	return &Runner{config: config, store: store, qbit: qbit, transfer: transferClient, pesto: pestoClient, ingest: ingestClient, logger: logger}
}

func (r *Runner) Initialize(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Join(r.config.Worker.DataDir, "jobs"), 0o700); err != nil {
		return fmt.Errorf("create worker jobs directory: %w", err)
	}
	transferCtx, transferCancel := context.WithTimeout(ctx, 20*time.Second)
	defer transferCancel()
	if err := r.transfer.Initialize(transferCtx); err != nil {
		return err
	}
	validationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := r.pesto.ValidateBinary(validationCtx); err != nil {
		return err
	}
	r.pestoVersion = r.pesto.Version(validationCtx)
	count, err := r.store.ReconcileInterruptedPosts(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		r.logger.Warn("Pesto jobs require manual reconciliation after worker restart", "job_count", count)
	}
	return nil
}

func (r *Runner) RunOnce(ctx context.Context, selectedHash string) error {
	job, err := r.store.NextRunnable(ctx)
	if err != nil {
		return err
	}
	if job == nil {
		job, err = r.discover(ctx, selectedHash)
		if err != nil || job == nil {
			return err
		}
	}
	return r.process(ctx, job)
}

func (r *Runner) discover(ctx context.Context, selectedHash string) (*jobs.Job, error) {
	selectedHash = strings.ToLower(strings.TrimSpace(selectedHash))
	if selectedHash != "" && !torrentHashPattern.MatchString(selectedHash) {
		return nil, errors.New("selected torrent hash must be a 40- or 64-character hexadecimal info hash")
	}
	if err := r.qbit.Login(ctx); err != nil {
		return nil, err
	}
	torrents, err := r.qbit.Completed(ctx, r.config.QBittorrent.CandidateTag, selectedHash)
	if err != nil {
		return nil, err
	}
	for _, torrent := range torrents {
		remotePath, localName, err := r.transfer.ResolveSource(torrent.ContentPath, torrent.SavePath, torrent.Name)
		if err != nil {
			r.logger.Warn("skipping unsafe qBittorrent content path", "torrent_hash", torrent.Hash, "error", err)
			continue
		}
		jobID := ksuid.New().String()
		workspace := filepath.Join(r.config.Worker.DataDir, "jobs", jobID)
		inputPath, err := r.transfer.InputPath(workspace, remotePath, localName)
		if err != nil {
			r.logger.Warn("skipping unresolved qBittorrent content path", "torrent_hash", torrent.Hash, "error", err)
			continue
		}
		job, created, err := r.store.Reserve(ctx, jobs.CreateInput{
			JobID: jobID, TorrentHash: torrent.Hash, ReleaseName: torrent.Name,
			SourceTracker: qbittorrent.TrackerIdentity(torrent.Tracker), SourcePath: remotePath, SourceSize: torrent.Size,
			WorkspacePath: workspace, InputPath: inputPath,
		})
		if err != nil {
			return nil, err
		}
		if created {
			r.logger.Info("reserved completed torrent", "job_id", job.ID, "torrent_hash", job.TorrentHash, "release_name", job.ReleaseName, "source_size", job.SourceSize)
			return job, nil
		}
	}
	r.logger.Debug("no eligible completed torrent found")
	return nil, nil
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	if job.State == jobs.StateFailed {
		switch job.RetryFrom {
		case jobs.StateTransferring:
			job.State = jobs.StateReserved
		case jobs.StateTransferred:
			job.State = jobs.StateTransferred
		case jobs.StateSubmitting:
			job.State = jobs.StateNZBReady
		default:
			return fmt.Errorf("job %s has unsupported retry checkpoint %q", job.ID, job.RetryFrom)
		}
	}

	if job.State == jobs.StateReserved || job.State == jobs.StateTransferring {
		if err := r.transferJob(ctx, job); err != nil {
			_ = r.store.MarkRetryableFailure(ctx, job.ID, jobs.StateTransferring, err)
			return err
		}
		job.State = jobs.StateTransferred
	}
	if job.State == jobs.StateTransferred {
		if err := r.postJob(ctx, job); err != nil {
			return err
		}
		job.State = jobs.StateNZBReady
	}
	if job.State == jobs.StateNZBReady || job.State == jobs.StateSubmitting {
		if err := r.submitJob(ctx, job); err != nil {
			_ = r.store.MarkRetryableFailure(ctx, job.ID, jobs.StateSubmitting, err)
			return err
		}
	}
	return nil
}

func (r *Runner) transferJob(ctx context.Context, job *jobs.Job) error {
	if err := ensureDiskSpace(r.config.Worker.DataDir, job.SourceSize, r.config.Worker.MinFreeSpaceGB, r.config.Worker.WorkspaceMultiplier); err != nil {
		return fmt.Errorf("job %s disk guard: %w", job.ID, err)
	}
	if err := r.store.MarkTransferStarted(ctx, job.ID); err != nil {
		return err
	}
	r.logger.Info("preparing seedbox release", "job_id", job.ID, "torrent_hash", job.TorrentHash, "release_name", job.ReleaseName, "state", jobs.StateTransferring, "source_mode", r.transfer.Mode())
	bytes, err := r.transfer.Prepare(ctx, job.SourcePath, filepath.Join(job.WorkspacePath, "source"), job.InputPath)
	if err != nil {
		return fmt.Errorf("job %s transfer: %w", job.ID, err)
	}
	if bytes != job.SourceSize {
		return fmt.Errorf("job %s transferred size mismatch: qBittorrent=%d local=%d", job.ID, job.SourceSize, bytes)
	}
	if err := r.store.MarkTransferred(ctx, job.ID, bytes); err != nil {
		return err
	}
	job.BytesTransferred = bytes
	r.logger.Info("release source verified", "job_id", job.ID, "torrent_hash", job.TorrentHash, "state", jobs.StateTransferred, "source_mode", r.transfer.Mode(), "bytes_available", bytes)
	return nil
}

func (r *Runner) postJob(ctx context.Context, job *jobs.Job) error {
	bytes, err := r.transfer.Verify(ctx, job.InputPath)
	if err != nil || bytes != job.SourceSize {
		if err == nil {
			err = fmt.Errorf("prepared source size changed: expected=%d actual=%d", job.SourceSize, bytes)
		}
		_ = r.store.MarkRetryableFailure(ctx, job.ID, jobs.StateTransferred, err)
		return fmt.Errorf("job %s source verification before Pesto: %w", job.ID, err)
	}
	output := filepath.Join(job.WorkspacePath, "output", naming.NZBFilename(job.ReleaseName))
	result, err := r.pesto.Post(ctx, pesto.PostRequest{
		InputPath: job.InputPath, OutputNZB: output, Name: job.ReleaseName,
		OnStarted: func(pid int, started time.Time) error {
			return r.store.MarkPestoStarted(ctx, job.ID, pid, started)
		},
	})
	if err != nil {
		if result == nil {
			_ = r.store.MarkRetryableFailure(ctx, job.ID, jobs.StateTransferred, err)
			return fmt.Errorf("job %s could not start Pesto: %w", job.ID, err)
		}
		exitCode := result.ExitCode
		_ = r.store.MarkReconciliationRequired(ctx, job.ID, &exitCode, err)
		return fmt.Errorf("job %s Pesto outcome is ambiguous; manual reconciliation required: %w", job.ID, err)
	}
	if err := r.store.MarkPestoComplete(ctx, job.ID, result.NZBPath, result.Password, result.ExitCode, result.CompletedAt); err != nil {
		_ = r.store.MarkReconciliationRequired(ctx, job.ID, &result.ExitCode, err)
		return err
	}
	job.NZBPath, job.ArchivePassword, job.PestoCompletedAt = result.NZBPath, result.Password, &result.CompletedAt
	r.logger.Info("Pesto posting completed", "job_id", job.ID, "torrent_hash", job.TorrentHash, "state", jobs.StateNZBReady, "pesto_exit_code", result.ExitCode, "duration", result.Duration, "bytes_posted", result.BytesPosted, "article_count", result.ArticleCount)
	return nil
}

func (r *Runner) submitJob(ctx context.Context, job *jobs.Job) error {
	if err := r.store.MarkSubmitting(ctx, job.ID); err != nil {
		return err
	}
	postedAt := time.Now().UTC()
	if job.PestoCompletedAt != nil {
		postedAt = job.PestoCompletedAt.UTC()
	}
	result, err := r.ingest.Submit(ctx, ingest.Request{
		JobID: job.ID, NZBPath: job.NZBPath, ReleaseName: job.ReleaseName,
		TorrentHash: job.TorrentHash, SourceTracker: job.SourceTracker, SourceSize: job.SourceSize,
		PostedAt: postedAt, WorkerNodeID: r.config.Worker.NodeID, Password: job.ArchivePassword,
		PestoVersion: r.pestoVersion, Obfuscated: r.config.Pesto.Obfuscation != "none",
		Encrypted: r.config.Pesto.Encryption, HasPAR2: r.config.Pesto.PAR2Percent > 0,
	})
	if err != nil {
		return fmt.Errorf("job %s GoNZB ingest: %w", job.ID, err)
	}
	if err := r.store.MarkComplete(ctx, job.ID, result.ReleaseID, time.Now().UTC()); err != nil {
		return err
	}
	r.logger.Info("GoNZB ingest confirmed", "job_id", job.ID, "torrent_hash", job.TorrentHash, "state", jobs.StateComplete, "gonzb_release_id", result.ReleaseID, "created", result.Created)
	if r.config.Worker.CleanupOnSuccess {
		if err := removeWorkspace(r.config.Worker.DataDir, job.WorkspacePath); err != nil {
			r.logger.Warn("completed job workspace cleanup failed", "job_id", job.ID, "error", err)
		}
	}
	return nil
}

func ensureDiskSpace(dataDir string, sourceBytes int64, minimumGB, multiplier float64) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(dataDir, &stats); err != nil {
		return err
	}
	available := int64(stats.Bavail) * int64(stats.Bsize)
	minimum := int64(minimumGB * 1024 * 1024 * 1024)
	required := minimum + int64(float64(sourceBytes)*multiplier)
	if available < required {
		return fmt.Errorf("insufficient free space: available=%d required=%d", available, required)
	}
	return nil
}

func removeWorkspace(dataDir, workspace string) error {
	jobsRoot, err := filepath.Abs(filepath.Join(dataDir, "jobs"))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(jobsRoot, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to clean workspace outside worker jobs directory: %q", target)
	}
	return os.RemoveAll(target)
}
