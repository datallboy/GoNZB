package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Getenv("GONZB_RSYNC_FIXTURE_SOURCE_ROOT"), os.Getenv("GONZB_RSYNC_FIXTURE_DEST_ROOT")); err != nil {
		fmt.Fprintln(os.Stderr, "rsyncfixture:", err)
		os.Exit(1)
	}
}

func run(args []string, sourceRoot, destinationRoot string) error {
	if !filepath.IsAbs(sourceRoot) || !filepath.IsAbs(destinationRoot) {
		return errors.New("absolute GONZB_RSYNC_FIXTURE_SOURCE_ROOT and GONZB_RSYNC_FIXTURE_DEST_ROOT are required")
	}
	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(args) != separator+3 {
		return errors.New("expected worker rsync options followed by -- SOURCE DESTINATION")
	}
	if !contains(args[:separator], "-a") || !contains(args[:separator], "--partial") || !contains(args[:separator], "--protect-args") || !contains(args[:separator], "--stats") {
		return errors.New("worker safety and resumability rsync options are missing")
	}
	remote := args[separator+1]
	destination := filepath.Clean(args[separator+2])
	colon := strings.IndexByte(remote, ':')
	if colon <= 0 || !strings.Contains(remote[:colon], "@") {
		return errors.New("source must use user@host:absolute-path syntax")
	}
	source := filepath.Clean(remote[colon+1:])
	if !filepath.IsAbs(source) || !within(sourceRoot, source, false) {
		return fmt.Errorf("source %q escapes fixture root", source)
	}
	if !filepath.IsAbs(destination) || !within(destinationRoot, destination, true) {
		return fmt.Errorf("destination %q escapes fixture root", destination)
	}
	return copyTree(source, filepath.Join(destination, filepath.Base(source)))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func within(root, target string, allowRoot bool) bool {
	root, rootErr := filepath.Abs(root)
	target, targetErr := filepath.Abs(target)
	if rootErr != nil || targetErr != nil {
		return false
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return allowRoot || relative != "."
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture refuses symbolic link %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("fixture refuses non-regular file %s", path)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
