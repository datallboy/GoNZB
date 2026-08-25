package transfer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type mountInfo struct {
	MountPath string
	Options   map[string]bool
	FSType    string
	Source    string
}

func (c *Client) Initialize(ctx context.Context) error {
	switch c.config.Type {
	case "rsync":
		if _, err := exec.LookPath(c.config.Binary); err != nil {
			return fmt.Errorf("find rsync binary: %w", err)
		}
		return nil
	case "sshfs":
		return c.initializeSSHFS(ctx)
	default:
		return fmt.Errorf("unsupported transfer type %q", c.config.Type)
	}
}

func (c *Client) Close(ctx context.Context) error {
	if c.config.Type != "sshfs" || !c.mountedByClient || !c.config.UnmountOnExit {
		return nil
	}
	info, mounted, err := findMount(filepath.Clean(c.config.MountPath))
	if err != nil {
		return err
	}
	if !mounted {
		c.mountedByClient = false
		return nil
	}
	if err := c.validateMountedSSHFS(info); err != nil {
		return fmt.Errorf("refusing to unmount a replaced source mount: %w", err)
	}
	cmd := exec.CommandContext(ctx, c.config.UnmountBinary, "-u", filepath.Clean(c.config.MountPath))
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unmount SSHFS source: %w: %s", err, output.String())
	}
	c.mountedByClient = false
	return nil
}

func (c *Client) initializeSSHFS(ctx context.Context) error {
	mountPath := filepath.Clean(c.config.MountPath)
	if err := os.MkdirAll(mountPath, 0o700); err != nil {
		return fmt.Errorf("create SSHFS mount path: %w", err)
	}
	pathInfo, err := os.Lstat(mountPath)
	if err != nil {
		return fmt.Errorf("inspect SSHFS mount path: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("SSHFS mount path must not be a symbolic link: %s", mountPath)
	}
	info, mounted, err := findMount(mountPath)
	if err != nil {
		return err
	}
	if mounted {
		return c.validateMountedSSHFS(info)
	}
	if !c.config.ManageMount {
		return fmt.Errorf("SSHFS source is not mounted at %s and transfer.manage_mount is false", mountPath)
	}
	if _, err := exec.LookPath(c.config.SSHFSBinary); err != nil {
		return fmt.Errorf("find SSHFS binary: %w", err)
	}
	args, err := c.sshfsArguments()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.config.SSHFSBinary, args...)
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mount SSHFS source: %w: %s", err, output.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		info, mounted, err = findMount(mountPath)
		if err != nil {
			return err
		}
		if mounted {
			c.mountedByClient = true
			if err := c.validateMountedSSHFS(info); err != nil {
				return err
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("SSHFS command completed but %s did not become a mount point", mountPath)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *Client) sshfsArguments() ([]string, error) {
	args := []string{
		c.config.User + "@" + c.config.Host + ":" + filepath.Clean(c.config.SourceRoot),
		filepath.Clean(c.config.MountPath),
		"-p", strconv.Itoa(c.config.Port),
		"-o", "ro",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "reconnect",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	}
	if key := strings.TrimSpace(c.config.KeyPath); key != "" {
		if strings.ContainsAny(key, "\r\n\x00") {
			return nil, errors.New("SSH key path contains invalid characters")
		}
		args = append(args, "-o", "IdentityFile="+key)
	}
	for _, option := range c.config.SSHFSOptions {
		args = append(args, "-o", option)
	}
	return args, nil
}

func (c *Client) verifySSHFS() error {
	info, mounted, err := findMount(filepath.Clean(c.config.MountPath))
	if err != nil {
		return err
	}
	if !mounted {
		return fmt.Errorf("SSHFS source is no longer mounted at %s", c.config.MountPath)
	}
	return c.validateMountedSSHFS(info)
}

func (c *Client) ensureSSHFS(ctx context.Context) error {
	err := c.verifySSHFS()
	if err == nil || !c.config.ManageMount {
		return err
	}
	_, mounted, mountErr := findMount(filepath.Clean(c.config.MountPath))
	if mountErr != nil {
		return mountErr
	}
	if mounted {
		// Never replace a mount that exists but fails source/type/read-only
		// validation. It may belong to the operator or another service.
		return err
	}
	return c.initializeSSHFS(ctx)
}

func (c *Client) validateMountedSSHFS(info mountInfo) error {
	if err := validateMountedSSHFS(info); err != nil {
		return err
	}
	expected := c.config.User + "@" + c.config.Host + ":" + filepath.Clean(c.config.SourceRoot)
	if info.Source != expected {
		return fmt.Errorf("SSHFS mount at %s exposes %q, expected %q", info.MountPath, info.Source, expected)
	}
	return nil
}

func validateMountedSSHFS(info mountInfo) error {
	if info.FSType != "fuse.sshfs" {
		return fmt.Errorf("mount path %s uses %s, not SSHFS", info.MountPath, info.FSType)
	}
	if !info.Options["ro"] {
		return fmt.Errorf("SSHFS mount at %s is not read-only", info.MountPath)
	}
	return nil
}

func findMount(target string) (mountInfo, bool, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return mountInfo{}, false, fmt.Errorf("read Linux mount table: %w", err)
	}
	defer file.Close()
	target, err = filepath.Abs(target)
	if err != nil {
		return mountInfo{}, false, err
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		info, ok := parseMountInfoLine(scanner.Text())
		if !ok {
			continue
		}
		path, err := filepath.Abs(info.MountPath)
		if err == nil && path == target {
			return info, true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return mountInfo{}, false, fmt.Errorf("scan Linux mount table: %w", err)
	}
	return mountInfo{}, false, nil
}

func parseMountInfoLine(line string) (mountInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return mountInfo{}, false
	}
	separator := -1
	for i, field := range fields {
		if field == "-" {
			separator = i
			break
		}
	}
	if separator < 6 || separator+2 >= len(fields) {
		return mountInfo{}, false
	}
	options := make(map[string]bool)
	for _, option := range strings.Split(fields[5], ",") {
		options[option] = true
	}
	if separator+3 < len(fields) {
		for _, option := range strings.Split(fields[separator+3], ",") {
			options[option] = true
		}
	}
	return mountInfo{
		MountPath: decodeMountPath(fields[4]), Options: options, FSType: fields[separator+1], Source: decodeMountPath(fields[separator+2]),
	}, true
}

func decodeMountPath(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}

func validateSSHFSOption(option string) error {
	option = strings.TrimSpace(option)
	if option == "" || strings.HasPrefix(option, "-") || strings.ContainsAny(option, "\r\n\x00") {
		return errors.New("SSHFS options must be non-empty single-line values")
	}
	for _, component := range strings.Split(option, ",") {
		key := strings.ToLower(strings.TrimSpace(strings.SplitN(component, "=", 2)[0]))
		switch key {
		case "rw", "ro", "allow_other", "allow_root", "identityfile", "port", "password_stdin", "ssh_command", "command", "batchmode", "connecttimeout":
			return fmt.Errorf("SSHFS option %q conflicts with worker-managed read-only mount safety", key)
		}
	}
	return nil
}
