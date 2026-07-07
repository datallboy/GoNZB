//go:build !unix

package pgindex

import (
	"fmt"
	"path/filepath"
)

func populateDatabaseStorageFilesystemStatus(status *DatabaseStorageStatus, dataDirectory string) error {
	if status == nil {
		return fmt.Errorf("database storage status is nil")
	}
	status.DataDirectory = filepath.Clean(dataDirectory)
	status.FilesystemVisible = false
	return nil
}
