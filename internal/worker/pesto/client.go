package pesto

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/nzb"
)

type Config struct {
	Binary      string
	ConfigPath  string
	Compression string
	Encryption  bool
	Obfuscation string
	PAR2Percent int
	ExtraArgs   []string
}

type PostRequest struct {
	InputPath string
	OutputNZB string
	Name      string
	OnStarted func(pid int, started time.Time) error
}

type PostResult struct {
	NZBPath      string
	Password     string
	BytesPosted  int64
	ArticleCount int
	ExitCode     int
	StartedAt    time.Time
	CompletedAt  time.Time
	Duration     time.Duration
}

type Client struct{ config Config }

func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.Binary) == "" {
		return nil, errors.New("pesto binary is required")
	}
	for _, arg := range config.ExtraArgs {
		key := strings.SplitN(arg, "=", 2)[0]
		switch key {
		case "--out", "-o", "--nzb-dir", "--nzb-title", "--nzb-name", "--nzb-password", "--nzb-conflict",
			"--output-format", "--password", "--compress", "--compress-temp-dir", "--obfuscate",
			"--par2", "--post-hook", "--pre-hook", "--watch", "--each", "--season", "--jobs",
			"--config", "-c", "--dry-run", "--par2-only", "--resume", "--merge-season", "--no-overwrite":
			return nil, fmt.Errorf("pesto extra argument %q conflicts with worker-managed posting lifecycle", key)
		}
	}
	return &Client{config: config}, nil
}

// ValidateBinary prevents accidentally invoking the unrelated Linux networking
// utility that is also distributed under the name "pesto".
func (c *Client) ValidateBinary(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, c.config.Binary, "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run configured Pesto help: %w", err)
	}
	help := string(output)
	for _, required := range []string{"--output-format", "--obfuscate", "--password", "--par2", "--nzb-title"} {
		if !strings.Contains(help, required) {
			return fmt.Errorf("configured binary %q is not the supported Usenet Pesto CLI (missing %s)", c.config.Binary, required)
		}
	}
	return nil
}

func (c *Client) Version(ctx context.Context) string {
	output, err := exec.CommandContext(ctx, c.config.Binary, "--version").Output()
	if err != nil {
		return "unknown"
	}
	line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	if len(line) > 128 || line == "" {
		return "unknown"
	}
	return line
}

func (c *Client) Post(ctx context.Context, request PostRequest) (*PostResult, error) {
	args, err := c.arguments(request)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(request.OutputNZB), 0o700); err != nil {
		return nil, fmt.Errorf("create Pesto output directory: %w", err)
	}
	cmd := exec.CommandContext(ctx, c.config.Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Pesto stdout: %w", err)
	}
	cmd.Stderr = io.Discard // Diagnostics can include credentials or the generated archive password.
	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Pesto: %w", err)
	}
	result := &PostResult{NZBPath: request.OutputNZB, StartedAt: started}
	if request.OnStarted != nil {
		if err := request.OnStarted(cmd.Process.Pid, started); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return result, fmt.Errorf("persist Pesto process state: %w", err)
		}
	}

	parseErr := parseEvents(stdout, result)
	waitErr := cmd.Wait()
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(started)
	result.ExitCode = exitCode(waitErr)
	if parseErr != nil {
		return result, parseErr
	}
	if waitErr != nil {
		return result, fmt.Errorf("pesto exited with code %d", result.ExitCode)
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("pesto exited with code %d", result.ExitCode)
	}
	info, err := os.Stat(request.OutputNZB)
	if err != nil || info.Size() == 0 {
		return result, errors.New("pesto completed without producing a non-empty NZB")
	}
	password, err := sanitizeNZBFile(request.OutputNZB)
	if err != nil {
		return result, fmt.Errorf("sanitize generated NZB: %w", err)
	}
	if c.config.Encryption && password == "" {
		return result, errors.New("pesto encryption was enabled but the generated NZB has no password metadata")
	}
	result.Password = password
	return result, nil
}

func (c *Client) arguments(request PostRequest) ([]string, error) {
	if !filepath.IsAbs(request.InputPath) || !filepath.IsAbs(request.OutputNZB) {
		return nil, errors.New("pesto input and output paths must be absolute")
	}
	if strings.TrimSpace(request.Name) == "" {
		return nil, errors.New("pesto NZB name is required")
	}
	args := make([]string, 0, len(c.config.ExtraArgs)+16)
	if c.config.ConfigPath != "" {
		args = append(args, "--config="+c.config.ConfigPath)
	}
	args = append(args, c.config.ExtraArgs...)
	args = append(args,
		"--out", request.OutputNZB,
		"--nzb-conflict=fail",
		"--output-format", "json",
		"--no-history", "--no-notify", "--no-hooks",
		"--compress="+c.config.Compression,
		"--compress-temp-dir", filepath.Dir(request.OutputNZB),
		"--obfuscate="+c.config.Obfuscation,
		"--par2", strconv.Itoa(c.config.PAR2Percent),
	)
	if c.config.Encryption {
		args = append(args, "--password=")
	}
	args = append(args, request.InputPath)
	return args, nil
}

type event struct {
	Type     string  `json:"type"`
	Bytes    int64   `json:"bytes"`
	OK       bool    `json:"ok"`
	Failures int     `json:"failures"`
	Progress float64 `json:"progress_pct"`
	Path     string  `json:"path"`
}

func parseEvents(reader io.Reader, result *PostResult) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	finished := false
	var firstErr error
	for scanner.Scan() {
		line := scanner.Bytes()
		if isGeneratedPasswordNotice(line) {
			// Pesto 0.8.6 writes this secret-bearing status line to stdout even
			// in JSON mode. Deliberately discard it without copying it into an
			// error or log; the authoritative password comes from NZB metadata.
			continue
		}
		var item event
		if err := json.Unmarshal(line, &item); err != nil {
			if firstErr == nil {
				firstErr = errors.New("pesto emitted invalid structured output")
			}
			continue
		}
		switch item.Type {
		case "segment_done":
			if item.OK {
				result.BytesPosted += item.Bytes
				result.ArticleCount++
			}
		case "finished":
			// Pesto 0.8.6 can finish below 100% when its pre-seeded PAR2 byte
			// estimate exceeds the generated recovery volume. Its explicit ok
			// and failure fields are the completion contract; progress is only
			// a display estimate.
			finished = item.OK && item.Failures == 0
		case "nzb_written":
			if filepath.Clean(item.Path) != filepath.Clean(result.NZBPath) {
				if firstErr == nil {
					firstErr = errors.New("pesto reported an unexpected NZB output path")
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Pesto structured output: %w", err)
	}
	if firstErr != nil {
		return firstErr
	}
	if !finished {
		return errors.New("pesto output did not contain a successful finished event")
	}
	return nil
}

func isGeneratedPasswordNotice(line []byte) bool {
	const prefix = "archive password: "
	text := string(line)
	if !strings.HasPrefix(text, prefix) || len(text) != len(prefix)+24 {
		return false
	}
	for _, char := range text[len(prefix):] {
		if !(char >= 'A' && char <= 'Z') && !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func sanitizeNZBFile(filename string) (string, error) {
	payload, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	sanitized, password, err := nzb.SanitizeBytes(payload, nzb.DefaultLimits())
	if err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(filename), ".nzb-sanitize-*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return "", err
	}
	if _, err := temp.Write(sanitized); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempName, filename); err != nil {
		return "", err
	}
	return password, nil
}
