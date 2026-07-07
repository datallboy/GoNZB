//go:build unix

package pgindex

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func populateDatabaseStorageFilesystemStatus(status *DatabaseStorageStatus, dataDirectory string) error {
	if status == nil {
		return fmt.Errorf("database storage status is nil")
	}
	dataDir := filepath.Clean(dataDirectory)
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dataDir, &fs); err != nil {
		if os.IsNotExist(err) || err == syscall.ENOENT || err == syscall.EPERM || err == syscall.EACCES {
			status.DataDirectory = dataDir
			status.FilesystemVisible = false
			return nil
		}
		return fmt.Errorf("stat postgres data directory %s: %w", dataDir, err)
	}
	blockSize := uint64(fs.Bsize)
	status.FilesystemFreeBytes = int64(fs.Bavail * blockSize)
	status.FilesystemTotalBytes = int64(fs.Blocks * blockSize)
	if status.FilesystemTotalBytes > 0 {
		status.FilesystemFreePercent = (float64(status.FilesystemFreeBytes) / float64(status.FilesystemTotalBytes)) * 100
	}
	status.DataDirectory = dataDir
	status.FilesystemVisible = true
	return nil
}
