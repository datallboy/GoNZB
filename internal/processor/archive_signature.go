package processor

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

type archiveSignatureKind string

const (
	archiveSignatureUnknown archiveSignatureKind = ""
	archiveSignatureRAR     archiveSignatureKind = "rar"
	archiveSignature7z      archiveSignatureKind = "7z"
	archiveSignatureZIP     archiveSignatureKind = "zip"
)

func detectArchiveSignature(filePath string) (archiveSignatureKind, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return archiveSignatureUnknown, err
	}
	defer file.Close()

	header := make([]byte, 8)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return archiveSignatureUnknown, err
	}

	switch {
	case hasSignature(header[:n], rarSignatures):
		return archiveSignatureRAR, nil
	case hasSignature(header[:n], [][]byte{sevenZipSignature}):
		return archiveSignature7z, nil
	case hasSignature(header[:n], zipSignatures):
		return archiveSignatureZIP, nil
	default:
		return archiveSignatureUnknown, nil
	}
}

func hasSignature(header []byte, signatures [][]byte) bool {
	for _, sig := range signatures {
		if len(header) >= len(sig) && bytes.Equal(header[:len(sig)], sig) {
			return true
		}
	}
	return false
}

func isExtensionlessArchive(path string) bool {
	if filepath.Ext(filepath.Base(path)) != "" {
		return false
	}
	kind, err := detectArchiveSignature(path)
	return err == nil && kind != archiveSignatureUnknown
}
