package inspect

import (
	"fmt"
	"path"
	"strings"
)

func PreviewObjectKey(providerID int64, releaseID, contentType string) string {
	ext := ".jpg"
	switch strings.TrimSpace(contentType) {
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	return path.Join("releases", fmt.Sprintf("%d", providerID), strings.TrimSpace(releaseID), "preview"+ext)
}
