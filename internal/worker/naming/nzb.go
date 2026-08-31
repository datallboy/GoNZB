package naming

import (
	"path/filepath"
	"strings"
	"unicode"
)

const maxNZBNameRunes = 180

var sourceExtensions = map[string]struct{}{
	".7z": {}, ".avi": {}, ".epub": {}, ".flac": {}, ".iso": {},
	".m2ts": {}, ".m4v": {}, ".mka": {}, ".mkv": {}, ".mov": {},
	".mp3": {}, ".mp4": {}, ".mpeg": {}, ".mpg": {}, ".pdf": {},
	".rar": {}, ".tar": {}, ".tgz": {}, ".ts": {}, ".wav": {},
	".webm": {}, ".wmv": {}, ".zip": {},
}

// NZBFilename returns a portable, human-readable filename without changing the
// title stored in GoNZB metadata or the contents of the NZB itself.
func NZBFilename(releaseName string) string {
	name := strings.TrimSpace(releaseName)
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".nzb" {
		name = strings.TrimSpace(name[:len(name)-len(ext)])
	} else if _, ok := sourceExtensions[ext]; ok {
		name = strings.TrimSpace(name[:len(name)-len(ext)])
	}

	var builder strings.Builder
	runeCount := 0
	for _, r := range name {
		if runeCount >= maxNZBNameRunes {
			break
		}
		switch {
		case unicode.IsControl(r):
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			builder.WriteRune('_')
		default:
			builder.WriteRune(r)
		}
		runeCount++
	}

	name = strings.Trim(builder.String(), " .")
	if strings.Trim(name, " ._") == "" {
		name = "release"
	}
	return name + ".nzb"
}
