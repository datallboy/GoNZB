package releasetitle

import (
	"path/filepath"
	"regexp"
	"strings"
)

type InspectionCandidate struct {
	Source     string
	Value      string
	Confidence float64
}

type InspectionTitle struct {
	ReleaseTitle string
	DisplayTitle string
	Source       string
	Confidence   float64
}

var (
	multiSpaceRE = regexp.MustCompile(`\s+`)
	resolutionRE = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p)\b`)
	videoCodecRE = regexp.MustCompile(`(?i)\b(x265|h265|hevc|av1|x264|h264|xvid)\b`)
	audioCodecRE = regexp.MustCompile(`(?i)\b(truehd|atmos|dts[- ]?hd|dts|ddp|eac3|ac3|aac|flac|mp3)\b`)
	sourceTagRE  = regexp.MustCompile(`(?i)\b(remux|bluray|bdrip|webrip|web[- ]?dl|hdtv|dvdrip|cam)\b`)
	numericNoise = regexp.MustCompile(`^[a-f0-9]{8,}$`)
	longOpaqueRE = regexp.MustCompile(`(?i)^[a-z0-9]{12,}$`)
	dotsRE       = regexp.MustCompile(`\.+`)
	yearLineRE   = regexp.MustCompile(`\b(19|20)\d{2}\b`)
)

func ChooseBestInspectionTitle(sourceTitle string, candidates []InspectionCandidate) (InspectionTitle, bool) {
	best := InspectionTitle{}
	for _, candidate := range candidates {
		item, ok := normalizeInspectionTitleCandidate(candidate)
		if !ok {
			continue
		}
		if best.ReleaseTitle == "" || item.Confidence > best.Confidence || (item.Confidence == best.Confidence && titleLooksCloserToSource(item.DisplayTitle, sourceTitle, best.DisplayTitle)) {
			best = item
		}
	}
	return best, best.ReleaseTitle != ""
}

func ShouldAdoptInspectionTitle(sourceTitle string, candidate InspectionTitle) bool {
	if candidate.ReleaseTitle == "" || candidate.DisplayTitle == "" {
		return false
	}
	sourceTitle = strings.TrimSpace(sourceTitle)
	if sourceTitle != "" && looksReadableTitle(sourceTitle) && !looksObfuscatedTitle(sourceTitle) && !titlesLookRelated(candidate.DisplayTitle, sourceTitle) {
		return false
	}
	if candidate.Confidence >= 0.82 {
		return true
	}
	return candidate.Confidence >= 0.70 && (sourceTitle == "" || looksObfuscatedTitle(sourceTitle) || !looksReadableTitle(sourceTitle))
}

func NormalizeSearchTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = strings.ReplaceAll(v, "-", " ")
	return strings.Join(strings.Fields(v), " ")
}

func DisplayTitleStyle(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = multiSpaceRE.ReplaceAllString(v, " ")
	return strings.TrimSpace(v)
}

func normalizeInspectionTitleCandidate(candidate InspectionCandidate) (InspectionTitle, bool) {
	switch strings.TrimSpace(candidate.Source) {
	case "media_title":
		releaseTitle, displayTitle, ok := normalizePlainTitleCandidate(candidate.Value)
		if !ok {
			return InspectionTitle{}, false
		}
		return InspectionTitle{
			ReleaseTitle: releaseTitle,
			DisplayTitle: displayTitle,
			Source:       "media_title",
			Confidence:   clampConfidence(candidate.Confidence),
		}, true
	case "archive_entry":
		releaseTitle, displayTitle, ok := normalizePathTitleCandidate(candidate.Value)
		if !ok {
			return InspectionTitle{}, false
		}
		return InspectionTitle{
			ReleaseTitle: releaseTitle,
			DisplayTitle: displayTitle,
			Source:       "archive_entry",
			Confidence:   clampConfidence(candidate.Confidence),
		}, true
	case "nfo":
		releaseTitle, displayTitle, ok := extractNFOTitleCandidate(candidate.Value)
		if !ok {
			return InspectionTitle{}, false
		}
		return InspectionTitle{
			ReleaseTitle: releaseTitle,
			DisplayTitle: displayTitle,
			Source:       "nfo",
			Confidence:   clampConfidence(candidate.Confidence),
		}, true
	default:
		return InspectionTitle{}, false
	}
}

func normalizePlainTitleCandidate(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 180 {
		return "", "", false
	}
	display := DisplayTitleStyle(value)
	if !looksReadableTitle(display) {
		return "", "", false
	}
	return releaseTitleStyle(display), display, true
}

func normalizePathTitleCandidate(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	clean := strings.ReplaceAll(value, "\\", "/")
	base := filepath.Base(clean)
	parent := filepath.Base(filepath.Dir(clean))
	lowerPath := strings.ToLower(clean)
	lowerBase := strings.ToLower(base)
	if strings.Contains(lowerPath, "/sample/") || strings.Contains(lowerBase, "sample") {
		return "", "", false
	}

	stem := mediaTitleStem(base)
	if stem == "" && parent != "" && parent != "." {
		stem = mediaTitleStem(parent)
	}
	if stem == "" {
		return "", "", false
	}

	title := DisplayTitleStyle(stem)
	if !looksReadableTitle(title) {
		return "", "", false
	}
	return releaseTitleStyle(stem), title, true
}

func mediaTitleStem(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base := filepath.Base(value)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

func extractNFOTitleCandidate(text string) (string, string, bool) {
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		line = strings.Trim(line, "-_=*#[]() ")
		if line == "" || len(line) > 140 {
			continue
		}
		if !looksReadableTitle(line) {
			continue
		}
		if resolutionRE.MatchString(line) || sourceTagRE.MatchString(line) || videoCodecRE.MatchString(line) || strings.Contains(strings.ToLower(line), "s0") || yearLineRE.MatchString(line) {
			display := DisplayTitleStyle(line)
			return releaseTitleStyle(line), display, true
		}
	}
	return "", "", false
}

func titleLooksCloserToSource(candidateTitle, sourceTitle, currentBest string) bool {
	if titlesLookRelated(candidateTitle, sourceTitle) && !titlesLookRelated(currentBest, sourceTitle) {
		return true
	}
	if currentBest == "" {
		return true
	}
	return len(NormalizeSearchTitle(candidateTitle)) > len(NormalizeSearchTitle(currentBest))
}

func looksObfuscatedTitle(title string) bool {
	normalized := NormalizeSearchTitle(title)
	if normalized == "" {
		return false
	}
	condensed := strings.ReplaceAll(normalized, " ", "")
	if condensed == "" {
		return false
	}
	if numericNoise.MatchString(condensed) {
		return true
	}
	parts := strings.Fields(normalized)
	if len(parts) == 1 && longOpaqueRE.MatchString(parts[0]) {
		return true
	}
	hasSemanticToken := resolutionRE.MatchString(normalized) ||
		videoCodecRE.MatchString(normalized) ||
		audioCodecRE.MatchString(normalized) ||
		sourceTagRE.MatchString(normalized)
	return !hasSemanticToken && len(parts) <= 2 && len(parts) > 0 && longOpaqueRE.MatchString(parts[0])
}

func looksReadableTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	if looksObfuscatedTitle(title) {
		return false
	}
	normalized := NormalizeSearchTitle(title)
	if normalized == "" {
		return false
	}
	parts := strings.Fields(normalized)
	if len(parts) >= 2 {
		return true
	}
	return resolutionRE.MatchString(normalized) ||
		videoCodecRE.MatchString(normalized) ||
		audioCodecRE.MatchString(normalized) ||
		sourceTagRE.MatchString(normalized)
}

func titlesLookRelated(a, b string) bool {
	a = NormalizeSearchTitle(a)
	b = NormalizeSearchTitle(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aFields := strings.Fields(a)
	bFields := strings.Fields(b)
	if len(aFields) == 0 || len(bFields) == 0 {
		return false
	}
	matches := 0
	for _, left := range aFields {
		for _, right := range bFields {
			if left == right {
				matches++
				break
			}
		}
	}
	minFields := len(aFields)
	if len(bFields) < minFields {
		minFields = len(bFields)
	}
	return minFields > 0 && float64(matches)/float64(minFields) >= 0.6
}

func releaseTitleStyle(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "\\", ".")
	v = strings.ReplaceAll(v, "/", ".")
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.ReplaceAll(v, " ", ".")
	v = dotsRE.ReplaceAllString(v, ".")
	return strings.Trim(v, ".")
}

func clampConfidence(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
