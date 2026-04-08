package inspect

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	splitSevenZipRE = regexp.MustCompile(`(?i)\.7z\.\d{3}$`)
	splitZipRE      = regexp.MustCompile(`(?i)\.zip\.\d{3}$`)
	rarPartRE       = regexp.MustCompile(`(?i)\.part\d+\.rar$|\.r\d{2,3}$`)
)

func IsArchiveFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".rar") ||
		strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".7z") ||
		splitSevenZipRE.MatchString(lower) ||
		splitZipRE.MatchString(lower) ||
		rarPartRE.MatchString(lower)
}

func IsArchiveRepresentative(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case splitSevenZipRE.MatchString(lower), splitZipRE.MatchString(lower):
		return strings.HasSuffix(lower, ".001")
	case strings.HasSuffix(lower, ".part01.rar"), strings.HasSuffix(lower, ".part1.rar"):
		return true
	case rarPartRE.MatchString(lower):
		return false
	case strings.HasSuffix(lower, ".7z"), strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".rar"):
		return true
	default:
		return false
	}
}

func ArchiveFamilyKey(fileName string) string {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case splitSevenZipRE.MatchString(lower):
		return splitSevenZipRE.ReplaceAllString(lower, ".7z")
	case splitZipRE.MatchString(lower):
		return splitZipRE.ReplaceAllString(lower, ".zip")
	case strings.HasSuffix(lower, ".part01.rar"), strings.HasSuffix(lower, ".part1.rar"):
		idx := strings.LastIndex(lower, ".part")
		if idx > 0 {
			return lower[:idx] + ".rar"
		}
	case rarPartRE.MatchString(lower):
		return rarPartRE.ReplaceAllString(lower, ".rar")
	}
	return lower
}

func ArchiveProbePath(fileName string) string {
	family := ArchiveFamilyKey(fileName)
	if family == "" {
		family = strings.ToLower(strings.TrimSpace(fileName))
	}
	if ext := filepath.Ext(family); ext == "" {
		return family + ".archive"
	}
	return family
}

func ArchiveEntryNamesFromSummary(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	rawEntries, ok := payload["archive_entries"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawEntries))
	for _, item := range rawEntries {
		value := strings.TrimSpace(toString(item))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return uniqueStrings(out)
}

func BestMediaEntry(entries []string) string {
	bestPrimary := ""
	bestSample := ""
	for _, entry := range entries {
		name := strings.TrimSpace(entry)
		if name == "" {
			continue
		}
		if IsVideoFile(name) || IsAudioFile(name) {
			if isSampleArchiveEntry(name) {
				if bestSample == "" {
					bestSample = name
				}
				continue
			}
			return name
		}
		if isSampleArchiveEntry(name) {
			if bestSample == "" {
				bestSample = name
			}
			continue
		}
		if bestPrimary == "" {
			bestPrimary = name
		}
	}
	if bestPrimary != "" {
		return bestPrimary
	}
	return bestSample
}

func isSampleArchiveEntry(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(lower, "/sample/") ||
		strings.Contains(lower, ".sample.") ||
		strings.Contains(lower, "sample-")
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	var prev string
	for idx, value := range values {
		if idx == 0 || value != prev {
			out = append(out, value)
		}
		prev = value
	}
	return out
}

func toString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return ""
	}
}
