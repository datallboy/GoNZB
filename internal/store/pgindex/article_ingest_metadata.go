package pgindex

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type parsedArticleMetadata struct {
	FileName       string
	FileIndex      int
	FileTotal      int
	YEncPart       int
	YEncTotalParts int
	FileSize       int64
}

var (
	ingestQuotedFilenameRE = regexp.MustCompile(`"([^"]+)"`)
	ingestCounterPairRE    = regexp.MustCompile(`(?i)(?:\(|\[)\s*(\d{1,5})\s*/\s*(\d{1,5})\s*(?:\)|\])`)
	ingestYEncTailRE       = regexp.MustCompile(`(?i)\s+yenc.*$`)
	ingestYEncSizeRE       = regexp.MustCompile(`(?i)\byenc\s*\(\s*\d{1,5}\s*/\s*\d{1,5}\s*\)\s+(\d{1,18})\s*$`)
)

func parseArticleIngestMetadata(subject string) parsedArticleMetadata {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return parsedArticleMetadata{}
	}

	out := parsedArticleMetadata{}
	if match := ingestQuotedFilenameRE.FindStringSubmatch(subject); len(match) == 2 {
		out.FileName = strings.TrimSpace(match[1])
	}

	counters := parseIngestCounterPairs(subject)
	if len(counters) > 0 {
		if filePart, fileTotal := bestCounterBeforeYEncForIngest(subject, counters); fileTotal > 0 {
			out.FileIndex = filePart
			out.FileTotal = fileTotal
		}
		if yencPart, yencTotal := bestCounterAfterYEncForIngest(subject, counters); yencTotal > 0 {
			out.YEncPart = yencPart
			out.YEncTotalParts = yencTotal
		}
	}

	if match := ingestYEncSizeRE.FindStringSubmatch(subject); len(match) == 2 {
		if size, err := strconv.ParseInt(match[1], 10, 64); err == nil && size > 0 {
			out.FileSize = size
		}
	}

	out.FileName = strings.TrimSpace(strings.Trim(filepath.Clean(out.FileName), "."))
	if out.FileName == "" || out.FileName == string(filepath.Separator) {
		out.FileName = ""
	}

	return out
}

type ingestCounterPair struct {
	Part  int
	Total int
}

func parseIngestCounterPairs(subject string) []ingestCounterPair {
	matches := ingestCounterPairRE.FindAllStringSubmatch(subject, -1)
	out := make([]ingestCounterPair, 0, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		part, errPart := strconv.Atoi(match[1])
		total, errTotal := strconv.Atoi(match[2])
		if errPart != nil || errTotal != nil || part <= 0 || total <= 0 {
			continue
		}
		out = append(out, ingestCounterPair{Part: part, Total: total})
	}
	return out
}

func bestCounterBeforeYEncForIngest(subject string, counters []ingestCounterPair) (int, int) {
	idx := strings.LastIndex(strings.ToLower(subject), "yenc")
	if idx <= 0 {
		return 0, 0
	}
	return bestCounterForIngest(subject[:idx], counters)
}

func bestCounterAfterYEncForIngest(subject string, counters []ingestCounterPair) (int, int) {
	idx := strings.LastIndex(strings.ToLower(subject), "yenc")
	if idx < 0 || idx >= len(subject) {
		return 0, 0
	}
	return bestCounterForIngest(subject[idx:], counters)
}

func bestCounterForIngest(section string, counters []ingestCounterPair) (int, int) {
	local := parseIngestCounterPairs(section)
	best := ingestCounterPair{}
	if len(local) > 0 {
		for _, pair := range local {
			if best.Total == 0 || pair.Total > best.Total || (pair.Total == best.Total && pair.Part > best.Part) {
				best = pair
			}
		}
		return best.Part, best.Total
	}
	for _, pair := range counters {
		if best.Total == 0 || pair.Total > best.Total || (pair.Total == best.Total && pair.Part > best.Part) {
			best = pair
		}
	}
	return best.Part, best.Total
}
