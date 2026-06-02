package inspect

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

var (
	splitSevenZipRE = regexp.MustCompile(`(?i)\.7z\.\d{3}$`)
	splitZipRE      = regexp.MustCompile(`(?i)\.zip\.\d{3}$`)
	rarPartRE       = regexp.MustCompile(`(?i)\.part\d+\.rar$|\.r\d{2,3}$`)
	rarPartNumRE    = regexp.MustCompile(`(?i)\.part(\d+)\.rar$`)
	rarVolNumRE     = regexp.MustCompile(`(?i)\.r(\d{2,3})$`)
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

func ArchiveFamilyFiles(candidateFile string, files []pgindex.CatalogReleaseFile) []pgindex.CatalogReleaseFile {
	target := ArchiveFamilyKey(candidateFile)
	if target == "" {
		target = strings.ToLower(strings.TrimSpace(candidateFile))
	}

	out := make([]pgindex.CatalogReleaseFile, 0, len(files))
	for _, file := range files {
		if file.IsPars || !IsArchiveFile(file.FileName) {
			continue
		}
		if ArchiveFamilyKey(file.FileName) != target {
			continue
		}
		out = append(out, file)
	}
	if len(out) > 1 {
		sortArchiveFamilyFiles(out)
		return out
	}

	// Obfuscated split-RAR posts often randomize the stem per volume, so filename-family
	// matching alone treats every partNN.rar as its own archive. When the release clearly
	// contains a contiguous split-RAR set, group those volumes together at the release level.
	if fallback := obfuscatedSplitRARFiles(files); len(fallback) > 0 && obfuscatedSplitRARCandidate(candidateFile, fallback) {
		sortArchiveFamilyFiles(fallback)
		return fallback
	}

	sortArchiveFamilyFiles(out)
	return out
}

func sortArchiveFamilyFiles(files []pgindex.CatalogReleaseFile) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].FileIndex != files[j].FileIndex {
			return files[i].FileIndex < files[j].FileIndex
		}
		return strings.ToLower(files[i].FileName) < strings.ToLower(files[j].FileName)
	})
}

func obfuscatedSplitRARFiles(files []pgindex.CatalogReleaseFile) []pgindex.CatalogReleaseFile {
	partFiles := make([]pgindex.CatalogReleaseFile, 0, len(files))
	seenParts := map[int]struct{}{}
	maxPart := 0
	for _, file := range files {
		if file.IsPars {
			continue
		}
		partNum, ok := rarPartNumber(file.FileName)
		if !ok {
			continue
		}
		partFiles = append(partFiles, file)
		seenParts[partNum] = struct{}{}
		if partNum > maxPart {
			maxPart = partNum
		}
	}
	if len(partFiles) < 3 {
		return nil
	}
	if _, ok := seenParts[1]; !ok {
		return nil
	}
	// Require a mostly contiguous sequence so we do not accidentally merge unrelated
	// archives that merely happen to use partNN naming.
	missing := 0
	for n := 1; n <= maxPart; n++ {
		if _, ok := seenParts[n]; !ok {
			missing++
		}
	}
	if missing > maxPart/10 {
		return nil
	}
	return append([]pgindex.CatalogReleaseFile(nil), partFiles...)
}

func obfuscatedSplitRARCandidate(candidateFile string, family []pgindex.CatalogReleaseFile) bool {
	if len(family) == 0 {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(candidateFile))
	if _, ok := rarPartNumber(lower); ok {
		return true
	}
	// Allow a plain .rar candidate to collapse into the same split set when the release
	// clearly has a part01/partNN sequence; service-level dedupe will still prefer part01.
	return strings.HasSuffix(lower, ".rar")
}

func rarPartNumber(fileName string) (int, bool) {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if matches := rarPartNumRE.FindStringSubmatch(lower); len(matches) == 2 {
		value, err := strconv.Atoi(matches[1])
		if err == nil && value > 0 {
			return value, true
		}
	}
	if matches := rarVolNumRE.FindStringSubmatch(lower); len(matches) == 2 {
		value, err := strconv.Atoi(matches[1])
		if err == nil {
			return value + 2, true
		}
	}
	return 0, false
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

func BestPreviewImageEntry(entries []string) string {
	bestScreenshot := ""
	bestImage := ""
	for _, entry := range entries {
		name := strings.TrimSpace(entry)
		if name == "" || !isArchiveImageEntry(name) {
			continue
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "screenshot") || strings.Contains(lower, "screenshots") || strings.Contains(lower, "/screen") || strings.Contains(lower, "\\screen") {
			if bestScreenshot == "" {
				bestScreenshot = name
			}
			continue
		}
		if bestImage == "" {
			bestImage = name
		}
	}
	if bestScreenshot != "" {
		return bestScreenshot
	}
	return bestImage
}

func isArchiveImageEntry(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
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
