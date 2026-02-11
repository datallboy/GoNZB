package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// CalculateFileHash generates the SHA-256 fingerprint for the actual NZB bytes.
// This is used for content-based deduplication and manual upload IDs.
func CalculateFileHash(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GenerateCompositeID creates the SHA-256 PK for Indexer results.
// This ensure all IDs in the database are a consistent 64-character hex string.
func GenerateCompositeID(source, guid string) string {
	input := fmt.Sprintf("%s-%s", source, guid)
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}
