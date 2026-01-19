package processor

import (
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// sanitizeFileName removes Usenet metadata and OS-illegal characters
func sanitizeFileName(subject string) string {
	res := html.UnescapeString(subject)

	// Try pattern A: Contents inside double quotes
	firstQuote := strings.Index(res, "\"")
	lastQuote := strings.LastIndex(res, "\"")
	if firstQuote != -1 && lastQuote != -1 && firstQuote < lastQuote {
		res = res[firstQuote+1 : lastQuote]
	} else {
		// Try pattern B: Strip Usenet metadata (fallback)
		//  Removes (1/14) or [01/14] and the "yenc" suffix

		// Remove yenc
		reYenc := regexp.MustCompile(`(?i)\s+yenc.*$`)
		res = reYenc.ReplaceAllString(res, "")

		// Remove leading counters like [1/14]
		reLead := regexp.MustCompile(`^\[\d+/\d+\]\s+`)
		res = reLead.ReplaceAllString(res, "")
	}

	// Final cleanup: remove OS characters
	// Windows/Linux/macOS safety
	badChars := regexp.MustCompile(`[\\/:*?"<>|]`)
	res = badChars.ReplaceAllString(res, "_")

	return strings.TrimSpace(res)
}

// cleanupExtensions checks if a filename matches the user's cleanup list
func cleanupExtensions(fileName string, cleanupMap map[string]struct{}) bool {
	filenameLower := strings.ToLower(fileName)

	ext := filepath.Ext(filenameLower)
	_, exists := cleanupMap[ext]

	return exists
}

// moveCrossDevice handles moving files between different mount points/filesystems
func moveCrossDevice(sourcePath, destPath string) error {
	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	tempDest := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".tmp")

	// Create the destination file
	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Copy the contents. io.Copy is efficient as it uses small buffers
	// or sendfile(2) where available.
	_, err = io.Copy(dst, src)
	if err != nil {
		dst.Close()
		os.Remove(tempDest)
		return err
	}

	err = dst.Sync()
	if err != nil {
		return err
	}

	// Explicitly close before deleting the source
	src.Close()
	dst.Close()

	err = os.Rename(tempDest, destPath)
	if err != nil {
		os.Remove(tempDest)
		return err
	}

	// Remove the original file only after copy success
	return os.Remove(sourcePath)
}

// moveFile handles the logic of moving a file, falling back to cross-device copy if rename fails.
func moveFile(source, dest string) error {
	// Try simple rename first
	err := os.Rename(source, dest)
	if err == nil {
		return nil
	}

	// If it fails (likely cross-device), use our helper
	return moveCrossDevice(source, dest)
}
