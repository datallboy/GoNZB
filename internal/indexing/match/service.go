package match

import (
	"html"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// parsed subject result used by the Milestone 6 assembly layer
type Result struct {
	ReleaseName string
	ReleaseKey  string
	BinaryName  string
	BinaryKey   string
	FileName    string
	PartNumber  int
	TotalParts  int
	IsPars      bool
}

type Service struct{}

var (
	quotedFilenameRE = regexp.MustCompile(`"([^"]+)"`)
	partMarkerRE     = regexp.MustCompile(`(?i)(?:\(|\[)\s*(\d{1,5})\s*/\s*(\d{1,5})\s*(?:\)|\])`)
	partLooseRE      = regexp.MustCompile(`(?i)\b(\d{1,5})\s*/\s*(\d{1,5})\b`)
	yencTailRE       = regexp.MustCompile(`(?i)\s+yenc.*$`)
	multiSpaceRE     = regexp.MustCompile(`\s+`)
	separatorRE      = regexp.MustCompile(`[\[\]\(\)\{\}\-_=+,;:]+`)
	unsafeFileRE     = regexp.MustCompile(`[\\/:*?"<>|]+`)
	nonKeyCharsRE    = regexp.MustCompile(`[^\pL\pN]+`)
	parFileRE        = regexp.MustCompile(`(?i)\.par2$|\.vol\d+\+\d+\.par2$`)
)

func NewService() *Service {
	return &Service{}
}

// MatchSubject parses a Usenet subject into stable assembly keys.
// this always returns a deterministic fallback to avoid permanent
// reprocessing of unmatched article headers.
func (s *Service) MatchSubject(subject, messageID string) Result {
	clean := strings.TrimSpace(html.UnescapeString(subject))

	partNumber, totalParts := parsePartInfo(clean)
	fileName := extractQuotedFilename(clean)

	releaseName := deriveReleaseName(clean, fileName)
	if releaseName == "" {
		releaseName = fallbackReleaseName(clean, messageID)
	}

	if fileName == "" {
		fileName = fallbackFileName(releaseName, messageID)
	}

	fileName = sanitizeFileName(fileName)
	if fileName == "" {
		fileName = fallbackFileName(releaseName, messageID)
	}

	binaryName := fileName
	releaseKey := normalizeKey(releaseName)
	if releaseKey == "" {
		releaseKey = normalizeKey(fileName)
	}
	binaryKey := releaseKey + "::" + normalizeKey(fileName)

	return Result{
		ReleaseName: releaseName,
		ReleaseKey:  releaseKey,
		BinaryName:  binaryName,
		BinaryKey:   binaryKey,
		FileName:    fileName,
		PartNumber:  partNumber,
		TotalParts:  totalParts,
		IsPars:      parFileRE.MatchString(strings.ToLower(fileName)),
	}
}

func parsePartInfo(subject string) (int, int) {
	for _, re := range []*regexp.Regexp{partMarkerRE, partLooseRE} {
		m := re.FindStringSubmatch(subject)
		if len(m) != 3 {
			continue
		}

		part, err1 := strconv.Atoi(m[1])
		total, err2 := strconv.Atoi(m[2])
		if err1 != nil || err2 != nil || part <= 0 || total <= 0 {
			continue
		}
		if part > total {
			total = part
		}
		return part, total
	}

	return 1, 1
}

func extractQuotedFilename(subject string) string {
	all := quotedFilenameRE.FindAllStringSubmatch(subject, -1)
	if len(all) == 0 {
		return ""
	}

	// Prefer the last quoted value. Subjects often include release label(s)
	// before the actual payload filename.
	for i := len(all) - 1; i >= 0; i-- {
		name := sanitizeFileName(all[i][1])
		if name != "" {
			return name
		}
	}

	return ""
}

func deriveReleaseName(subject, fileName string) string {
	base := stripYEnc(subject)
	base = partMarkerRE.ReplaceAllString(base, " ")
	base = partLooseRE.ReplaceAllString(base, " ")
	if fileName != "" {
		base = strings.ReplaceAll(base, `"`+fileName+`"`, " ")
		base = strings.ReplaceAll(base, fileName, " ")
	}
	base = separatorRE.ReplaceAllString(base, " ")
	base = multiSpaceRE.ReplaceAllString(base, " ")
	base = strings.TrimSpace(base)

	if base != "" {
		return base
	}

	if fileName != "" {
		stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		stem = strings.TrimSpace(stem)
		if stem != "" {
			return stem
		}
	}

	return ""
}

func fallbackReleaseName(subject, messageID string) string {
	base := stripYEnc(subject)
	base = partMarkerRE.ReplaceAllString(base, " ")
	base = partLooseRE.ReplaceAllString(base, " ")
	base = separatorRE.ReplaceAllString(base, " ")
	base = multiSpaceRE.ReplaceAllString(base, " ")
	base = strings.TrimSpace(base)
	if base != "" {
		return base
	}

	msg := strings.TrimSpace(messageID)
	msg = strings.Trim(msg, "<>")
	if msg == "" {
		return "unknown-release"
	}
	return msg
}

func fallbackFileName(releaseName, messageID string) string {
	name := sanitizeFileName(releaseName)
	if name == "" {
		name = sanitizeFileName(strings.Trim(messageID, "<>"))
	}
	if name == "" {
		name = "unknown-release"
	}
	if filepath.Ext(name) == "" {
		name += ".bin"
	}
	return name
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(html.UnescapeString(name))
	if name == "" {
		return ""
	}

	name = unsafeFileRE.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	name = multiSpaceRE.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)

	if name == "" {
		return ""
	}

	return name
}

func stripYEnc(subject string) string {
	out := yencTailRE.ReplaceAllString(subject, "")
	out = strings.TrimSpace(out)
	return out
}

func normalizeKey(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = stripYEnc(v)
	v = nonKeyCharsRE.ReplaceAllString(v, " ")
	v = multiSpaceRE.ReplaceAllString(v, " ")
	return strings.TrimSpace(v)
}
