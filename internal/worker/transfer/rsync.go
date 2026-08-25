package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var sshToken = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Config struct {
	Type          string
	Binary        string
	SSHFSBinary   string
	UnmountBinary string
	Host          string
	User          string
	Port          int
	KeyPath       string
	SourceRoot    string
	MountPath     string
	ManageMount   bool
	UnmountOnExit bool
	ExtraArgs     []string
	SSHFSOptions  []string
}

type Client struct {
	config          Config
	mountedByClient bool
}

func New(config Config) (*Client, error) {
	if config.Type == "" {
		config.Type = "rsync"
	}
	if !sshToken.MatchString(config.Host) || !sshToken.MatchString(config.User) {
		return nil, errors.New("SSH host and user may contain only letters, numbers, dot, underscore, and hyphen")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("SSH port is invalid")
	}
	switch config.Type {
	case "rsync":
		if strings.TrimSpace(config.Binary) == "" {
			config.Binary = "rsync"
		}
		for _, arg := range config.ExtraArgs {
			key := strings.SplitN(arg, "=", 2)[0]
			if !strings.HasPrefix(key, "-") {
				return nil, fmt.Errorf("rsync extra argument %q is not an option", arg)
			}
			switch key {
			case "-e", "--rsh", "--rsync-path", "--remove-source-files":
				return nil, fmt.Errorf("rsync extra argument %q conflicts with safe read-only transfer", key)
			}
		}
	case "sshfs":
		if strings.TrimSpace(config.SSHFSBinary) == "" {
			config.SSHFSBinary = "sshfs"
		}
		if strings.TrimSpace(config.UnmountBinary) == "" {
			config.UnmountBinary = "fusermount3"
		}
		if !filepath.IsAbs(config.MountPath) || filepath.Clean(config.MountPath) == string(filepath.Separator) {
			return nil, errors.New("SSHFS mount path must be an absolute non-root path")
		}
		for _, option := range config.SSHFSOptions {
			if err := validateSSHFSOption(option); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unsupported transfer type %q", config.Type)
	}
	return &Client{config: config}, nil
}

func (c *Client) Mode() string { return c.config.Type }

func (c *Client) ResolveSource(contentPath, savePath, releaseName string) (remotePath, localName string, err error) {
	source := strings.TrimSpace(contentPath)
	if source == "" {
		source = filepath.Join(strings.TrimSpace(savePath), releaseName)
	}
	source = filepath.Clean(source)
	root := filepath.Clean(c.config.SourceRoot)
	if !filepath.IsAbs(source) || !filepath.IsAbs(root) {
		return "", "", errors.New("qBittorrent content path and transfer source root must be absolute")
	}
	rel, err := filepath.Rel(root, source)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("qBittorrent content path %q is outside transfer source root %q", source, root)
	}
	name := filepath.Base(source)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "", "", fmt.Errorf("cannot derive a local name from qBittorrent content path %q", source)
	}
	return source, name, nil
}

func (c *Client) InputPath(workspacePath, remotePath, localName string) (string, error) {
	switch c.config.Type {
	case "rsync":
		return filepath.Join(workspacePath, "source", localName), nil
	case "sshfs":
		rel, err := filepath.Rel(filepath.Clean(c.config.SourceRoot), filepath.Clean(remotePath))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("mounted source path %q escapes configured source root", remotePath)
		}
		input := filepath.Join(c.config.MountPath, rel)
		mountRoot := filepath.Clean(c.config.MountPath)
		mountedRel, err := filepath.Rel(mountRoot, input)
		if err != nil || mountedRel == "." || mountedRel == ".." || strings.HasPrefix(mountedRel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("resolved SSHFS input %q escapes mount path", input)
		}
		return input, nil
	default:
		return "", fmt.Errorf("unsupported transfer type %q", c.config.Type)
	}
}

func (c *Client) Prepare(ctx context.Context, remotePath, destinationDir, inputPath string) (int64, error) {
	switch c.config.Type {
	case "rsync":
		return c.Pull(ctx, remotePath, destinationDir)
	case "sshfs":
		if err := c.ensureSSHFS(ctx); err != nil {
			return 0, err
		}
		expected, err := c.InputPath("", remotePath, filepath.Base(remotePath))
		if err != nil {
			return 0, err
		}
		if filepath.Clean(expected) != filepath.Clean(inputPath) {
			return 0, fmt.Errorf("durable job input path %q does not match mounted source %q", inputPath, expected)
		}
		bytes, err := treeSize(inputPath)
		if err != nil {
			return 0, fmt.Errorf("measure SSHFS source payload: %w", err)
		}
		return bytes, nil
	default:
		return 0, fmt.Errorf("unsupported transfer type %q", c.config.Type)
	}
}

func (c *Client) Verify(ctx context.Context, inputPath string) (int64, error) {
	if !filepath.IsAbs(inputPath) {
		return 0, errors.New("prepared input path must be absolute")
	}
	if c.config.Type == "sshfs" {
		if err := c.ensureSSHFS(ctx); err != nil {
			return 0, err
		}
		rel, err := filepath.Rel(filepath.Clean(c.config.MountPath), filepath.Clean(inputPath))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return 0, fmt.Errorf("prepared SSHFS input %q escapes mount path", inputPath)
		}
	}
	bytes, err := treeSize(inputPath)
	if err != nil {
		return 0, fmt.Errorf("verify prepared source payload: %w", err)
	}
	return bytes, nil
}

func (c *Client) Pull(ctx context.Context, remotePath, destinationDir string) (int64, error) {
	if c.config.Type != "rsync" {
		return 0, errors.New("rsync pull requested for a non-rsync transfer client")
	}
	if !filepath.IsAbs(remotePath) || !filepath.IsAbs(destinationDir) {
		return 0, errors.New("rsync source and destination must be absolute")
	}
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return 0, fmt.Errorf("create transfer destination: %w", err)
	}
	sshCommand, err := c.sshCommand()
	if err != nil {
		return 0, err
	}
	args := []string{"-a", "--partial", "--partial-dir=.rsync-partial", "--protect-args", "--stats", "-e", sshCommand}
	args = append(args, c.config.ExtraArgs...)
	args = append(args, "--", c.config.User+"@"+c.config.Host+":"+remotePath, destinationDir+string(filepath.Separator))
	cmd := exec.CommandContext(ctx, c.config.Binary, args...)
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("rsync pull failed: %w: %s", err, output.String())
	}
	localPath := filepath.Join(destinationDir, filepath.Base(remotePath))
	bytes, err := treeSize(localPath)
	if err != nil {
		return 0, fmt.Errorf("measure transferred payload: %w", err)
	}
	return bytes, nil
}

func (c *Client) sshCommand() (string, error) {
	parts := []string{"ssh", "-p", strconv.Itoa(c.config.Port), "-o", "BatchMode=yes"}
	if key := strings.TrimSpace(c.config.KeyPath); key != "" {
		if strings.ContainsAny(key, "\r\n\x00") {
			return "", errors.New("SSH key path contains invalid characters")
		}
		parts = append(parts, "-i", shellQuote(key))
	}
	return strings.Join(parts, " "), nil
}

func treeSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	const limit = 64 << 10
	original := len(p)
	if b.Len() >= limit {
		return original, nil
	}
	if len(p) > limit-b.Len() {
		p = p[:limit-b.Len()]
	}
	_, _ = b.Buffer.Write(p)
	return original, nil
}
