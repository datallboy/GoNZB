package tmdb

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

var (
	seasonEpisodeRE = regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`)
	dailyEpisodeRE  = regexp.MustCompile(`\b(20\d{2})[ ._-](\d{2})[ ._-](\d{2})\b`)
	yearRE          = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	qualityTokenRE  = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p|remux|bluray|bdrip|webrip|web[- ]?dl|web|hdtv|dvdrip|cam|x265|h265|hevc|av1|x264|h264|xvid|truehd|atmos|dts[- ]?hd|dts|ddp|eac3|ac3|aac|flac|mp3)\b`)
	separatorRE     = regexp.MustCompile(`[._/\\]+`)
	nonWordRE       = regexp.MustCompile(`[^a-z0-9 ]+`)
)

type releaseQuery struct {
	RawTitle     string
	ParsedFrom   string
	BaseTitle    string
	Year         int
	IsTV         bool
	Season       int
	Episode      int
	Confidence   float64
	DailyEpisode bool
}

type externalMatch struct {
	Source        string
	ExternalID    int64
	MediaType     string
	Title         string
	OriginalTitle string
	Year          int
	Confidence    float64
	Payload       map[string]any
}

func deriveReleaseQuery(candidate pgindex.ReleaseEnrichmentCandidate) (releaseQuery, bool) {
	raw := strings.TrimSpace(candidate.DeobfuscatedTitle)
	parsedFrom := "deobfuscated_title"
	if raw == "" {
		raw = strings.TrimSpace(candidate.Title)
		parsedFrom = "title"
	}
	if raw == "" {
		raw = strings.TrimSpace(candidate.SourceTitle)
		parsedFrom = "source_title"
	}
	if raw == "" {
		return releaseQuery{}, false
	}

	baseTitle, year, isTV, season, episode, dailyEpisode := parseReleaseQueryTitle(raw)
	if baseTitle == "" {
		return releaseQuery{}, false
	}

	confidence := 0.72
	if isTV {
		confidence = 0.80
	}
	return releaseQuery{
		RawTitle:     raw,
		ParsedFrom:   parsedFrom,
		BaseTitle:    baseTitle,
		Year:         year,
		IsTV:         isTV,
		Season:       season,
		Episode:      episode,
		Confidence:   confidence,
		DailyEpisode: dailyEpisode,
	}, true
}

func parseReleaseQueryTitle(raw string) (string, int, bool, int, int, bool) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", 0, false, 0, 0, false
	}
	clean = separatorRE.ReplaceAllString(clean, " ")
	clean = strings.Join(strings.Fields(clean), " ")

	if match := seasonEpisodeRE.FindStringSubmatchIndex(clean); len(match) >= 6 {
		base := strings.TrimSpace(clean[:match[0]])
		season, _ := strconv.Atoi(clean[match[2]:match[3]])
		episode, _ := strconv.Atoi(clean[match[4]:match[5]])
		return normalizeQueryTitle(base), 0, true, season, episode, false
	}
	if match := dailyEpisodeRE.FindStringSubmatchIndex(clean); len(match) >= 8 {
		base := strings.TrimSpace(clean[:match[0]])
		year, _ := strconv.Atoi(clean[match[2]:match[3]])
		return normalizeQueryTitle(base), year, true, 0, 0, true
	}

	qualityIdx := -1
	if loc := qualityTokenRE.FindStringIndex(clean); len(loc) == 2 {
		qualityIdx = loc[0]
	}

	year := 0
	if loc := yearRE.FindStringIndex(clean); len(loc) == 2 {
		if qualityIdx == -1 || loc[0] < qualityIdx {
			year, _ = strconv.Atoi(clean[loc[0]:loc[1]])
			return normalizeQueryTitle(clean[:loc[0]]), year, false, 0, 0, false
		}
	}

	if qualityIdx > 0 {
		return normalizeQueryTitle(clean[:qualityIdx]), 0, false, 0, 0, false
	}
	return normalizeQueryTitle(clean), 0, false, 0, 0, false
}

func normalizeQueryTitle(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = strings.Join(strings.Fields(v), " ")
	return strings.TrimSpace(v)
}

func normalizeCompareTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = separatorRE.ReplaceAllString(v, " ")
	v = nonWordRE.ReplaceAllString(v, " ")
	v = strings.Join(strings.Fields(v), " ")
	return strings.TrimSpace(v)
}

func rankExternalMatches(query releaseQuery, matches []externalMatch) []externalMatch {
	out := make([]externalMatch, 0, len(matches))
	for _, match := range matches {
		match.Confidence = scoreExternalMatch(query, match)
		out = append(out, match)
	}
	sortExternalMatches(out)
	return out
}

func bestExternalMatch(matches []externalMatch) (externalMatch, bool) {
	for _, match := range matches {
		if match.ExternalID > 0 {
			return match, true
		}
	}
	return externalMatch{}, false
}

func scoreExternalMatch(query releaseQuery, match externalMatch) float64 {
	bestTitle := match.Title
	bestScore := titleSimilarity(query.BaseTitle, match.Title)
	if alt := titleSimilarity(query.BaseTitle, match.OriginalTitle); alt > bestScore {
		bestScore = alt
		bestTitle = match.OriginalTitle
	}

	score := 0.35 + bestScore*0.55
	if normalizeCompareTitle(query.BaseTitle) == normalizeCompareTitle(bestTitle) {
		score += 0.12
	}
	if query.Year > 0 && match.Year > 0 {
		switch diff := absInt(query.Year - match.Year); {
		case diff == 0:
			score += 0.10
		case diff == 1:
			score += 0.04
		default:
			score -= 0.06
		}
	}
	if query.IsTV {
		if match.MediaType == "tv" {
			score += 0.08
		} else {
			score -= 0.10
		}
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

func titleSimilarity(a, b string) float64 {
	a = normalizeCompareTitle(a)
	b = normalizeCompareTitle(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(at))
	for _, token := range at {
		set[token] = struct{}{}
	}
	common := 0
	seen := map[string]struct{}{}
	for _, token := range bt {
		if _, ok := set[token]; ok {
			if _, dup := seen[token]; !dup {
				common++
				seen[token] = struct{}{}
			}
		}
	}
	denom := len(at)
	if len(bt) > denom {
		denom = len(bt)
	}
	return float64(common) / float64(denom)
}

func sortExternalMatches(matches []externalMatch) {
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Confidence > matches[i].Confidence {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
