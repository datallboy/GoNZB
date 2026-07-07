package predb

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

var (
	predbSeasonEpisodeRE = regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`)
	predbYearRE          = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	predbQualityRE       = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p|remux|bluray|bdrip|webrip|web[- ]?dl|web|hdtv|x265|h265|hevc|av1|x264|h264|xvid|truehd|atmos|dts[- ]?hd|dts|ddp|eac3|ac3|aac|flac|mp3)\b`)
	predbSepRE           = regexp.MustCompile(`[._/\\]+`)
	predbNonWordRE       = regexp.MustCompile(`[^a-z0-9 ]+`)
	predbLongOpaqueRE    = regexp.MustCompile(`(?i)^[a-z0-9]{12,}$`)
)

type MetadataFallbackQuery struct {
	IsTV           bool
	Year           int
	Season         int
	Episode        int
	PostedAt       *time.Time
	SizeBytes      int64
	PayloadSize    int64
	RuntimeSeconds int
	Resolution     string
	VideoCodec     string
	AudioCodec     string
}

type MetadataFallbackMatch struct {
	Entry      pgindex.PredbEntrySummary
	Confidence float64
}

const metadataFallbackAutoApplyWindow = 30 * time.Minute

func deriveQuery(candidate pgindex.ReleaseEnrichmentCandidate) (Query, bool) {
	var base string
	switch {
	case strings.TrimSpace(candidate.MatchedMediaTitle) != "":
		base = strings.TrimSpace(candidate.MatchedMediaTitle)
	case usablePredbBaseTitle(candidate.DeobfuscatedTitle):
		base = strings.TrimSpace(candidate.DeobfuscatedTitle)
	case usablePredbBaseTitle(candidate.Title):
		base = strings.TrimSpace(candidate.Title)
	case usablePredbBaseTitle(candidate.SourceTitle):
		base = strings.TrimSpace(candidate.SourceTitle)
	default:
		return Query{}, false
	}

	title, year, isTV, season, episode := parseQueryTokens(base)
	if title == "" {
		return Query{}, false
	}
	if !usablePredbParsedTitle(title) {
		return Query{}, false
	}

	year = firstNonZero(candidate.ExternalYear, year)
	season = firstNonZero(candidate.SeasonNumber, season)
	episode = firstNonZero(candidate.EpisodeNumber, episode)
	isTV = isTV || strings.TrimSpace(candidate.ExternalMediaType) == "tv"

	text := title
	if isTV && season > 0 && episode > 0 {
		text = fmt.Sprintf("%s S%02dE%02d", title, season, episode)
	} else if year > 0 {
		text = fmt.Sprintf("%s %d", title, year)
	}

	return Query{
		Text:              strings.TrimSpace(text),
		Title:             title,
		CanonicalTitle:    strings.TrimSpace(candidate.MatchedMediaTitle),
		Year:              year,
		IsTV:              isTV,
		Season:            season,
		Episode:           episode,
		PostedAt:          candidate.PostedAt,
		RuntimeSeconds:    candidate.RuntimeSeconds,
		Resolution:        strings.TrimSpace(candidate.PrimaryResolution),
		VideoCodec:        strings.TrimSpace(candidate.PrimaryVideoCodec),
		AudioCodec:        strings.TrimSpace(candidate.PrimaryAudioCodec),
		CurrentTitle:      strings.TrimSpace(candidate.Title),
		CurrentTitleSrc:   strings.TrimSpace(candidate.TitleSource),
		CurrentConfidence: candidate.IdentityConfidenceScore,
	}, true
}

func parseQueryTokens(raw string) (string, int, bool, int, int) {
	clean := strings.TrimSpace(raw)
	clean = predbSepRE.ReplaceAllString(clean, " ")
	clean = strings.Join(strings.Fields(clean), " ")
	if clean == "" {
		return "", 0, false, 0, 0
	}

	if match := predbSeasonEpisodeRE.FindStringSubmatchIndex(clean); len(match) >= 6 {
		base := strings.TrimSpace(clean[:match[0]])
		season, _ := strconv.Atoi(clean[match[2]:match[3]])
		episode, _ := strconv.Atoi(clean[match[4]:match[5]])
		return normalizeQueryTitle(base), 0, true, season, episode
	}

	qualityIdx := -1
	if loc := predbQualityRE.FindStringIndex(clean); len(loc) == 2 {
		qualityIdx = loc[0]
	}
	if loc := predbYearRE.FindStringIndex(clean); len(loc) == 2 {
		year, _ := strconv.Atoi(clean[loc[0]:loc[1]])
		if qualityIdx == -1 || loc[0] < qualityIdx {
			return normalizeQueryTitle(clean[:loc[0]]), year, false, 0, 0
		}
	}
	if qualityIdx > 0 {
		return normalizeQueryTitle(clean[:qualityIdx]), 0, false, 0, 0
	}
	return normalizeQueryTitle(clean), 0, false, 0, 0
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

func usablePredbBaseTitle(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if strings.EqualFold(v, "unknown-release") {
		return false
	}
	title, _, _, _, _ := parseQueryTokens(v)
	return usablePredbParsedTitle(title)
}

func usablePredbParsedTitle(v string) bool {
	normalized := normalizedTitle(v)
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, " ") {
		return true
	}
	condensed := strings.ReplaceAll(normalized, " ", "")
	if predbLongOpaqueRE.MatchString(condensed) {
		return false
	}
	return true
}

func normalizedTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = predbSepRE.ReplaceAllString(v, " ")
	v = predbNonWordRE.ReplaceAllString(v, " ")
	v = strings.Join(strings.Fields(v), " ")
	return strings.TrimSpace(v)
}

func displayTitle(v string) string {
	v = normalizeQueryTitle(v)
	return strings.TrimSpace(v)
}

func releaseTitle(v string) string {
	v = strings.ReplaceAll(strings.TrimSpace(v), " ", ".")
	v = strings.ReplaceAll(v, "_", ".")
	v = strings.Join(strings.FieldsFunc(v, func(r rune) bool { return r == '.' }), ".")
	return strings.Trim(v, ".")
}

func rankMatches(query Query, matches []Match) []Match {
	out := make([]Match, 0, len(matches))
	for _, match := range matches {
		match.Confidence = scoreMatch(query, match)
		out = append(out, match)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Confidence > out[i].Confidence {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func scoreMatch(query Query, match Match) float64 {
	score := 0.25 + titleSimilarity(query.Title, match.Title)*0.45
	normalizedMatch := normalizedTitle(match.Title)
	normalizedQuery := normalizedTitle(query.Title)
	if normalizedQuery != "" && strings.HasPrefix(normalizedMatch, normalizedQuery) {
		score += 0.22
	}
	if query.CanonicalTitle != "" {
		canonicalSimilarity := titleSimilarity(query.CanonicalTitle, match.Title)
		score += canonicalSimilarity * 0.20
		normalizedCanonical := normalizedTitle(query.CanonicalTitle)
		if normalizedCanonical != "" && strings.HasPrefix(normalizedMatch, normalizedCanonical) {
			score += 0.12
		}
	}
	if query.IsTV && query.Season > 0 && query.Episode > 0 {
		needle := fmt.Sprintf("s%02de%02d", query.Season, query.Episode)
		if strings.Contains(strings.ToLower(match.Title), needle) {
			score += 0.18
		} else {
			score -= 0.10
		}
	}
	if query.Year > 0 {
		if year := extractYear(match.Title); year > 0 {
			switch absInt(query.Year - year) {
			case 0:
				score += 0.10
			case 1:
				score += 0.04
			default:
				score -= 0.04
			}
		}
	}
	if query.Resolution != "" && strings.Contains(strings.ToLower(match.Title), strings.ToLower(query.Resolution)) {
		score += 0.04
	}
	if query.VideoCodec != "" && strings.Contains(strings.ToLower(match.Title), strings.ToLower(query.VideoCodec)) {
		score += 0.03
	}
	if query.AudioCodec != "" && strings.Contains(strings.ToLower(match.Title), strings.ToLower(query.AudioCodec)) {
		score += 0.02
	}
	if query.PostedAt != nil && match.PostedAt != nil {
		diff := query.PostedAt.Sub(*match.PostedAt)
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff <= 72*time.Hour:
			score += 0.08
		case diff <= 30*24*time.Hour:
			score += 0.04
		case diff > 365*24*time.Hour:
			score -= 0.08
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
	a = normalizedTitle(a)
	b = normalizedTitle(b)
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

func extractYear(v string) int {
	if loc := predbYearRE.FindStringIndex(v); len(loc) == 2 {
		year, _ := strconv.Atoi(v[loc[0]:loc[1]])
		return year
	}
	return 0
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func deriveMetadataFallbackQuery(candidate pgindex.ReleaseEnrichmentCandidate) (MetadataFallbackQuery, bool) {
	q := MetadataFallbackQuery{
		IsTV:           strings.TrimSpace(candidate.ExternalMediaType) == "tv" || (candidate.SeasonNumber > 0 && candidate.EpisodeNumber > 0),
		Year:           candidate.ExternalYear,
		Season:         candidate.SeasonNumber,
		Episode:        candidate.EpisodeNumber,
		PostedAt:       candidate.PostedAt,
		SizeBytes:      candidate.SizeBytes,
		PayloadSize:    firstNonZeroInt64(candidate.PayloadSizeBytes, candidate.SizeBytes),
		RuntimeSeconds: candidate.RuntimeSeconds,
		Resolution:     strings.TrimSpace(candidate.PrimaryResolution),
		VideoCodec:     strings.TrimSpace(candidate.PrimaryVideoCodec),
		AudioCodec:     strings.TrimSpace(candidate.PrimaryAudioCodec),
	}
	if q.PostedAt == nil && q.Year == 0 && q.Season == 0 && q.Episode == 0 && q.PayloadSize <= 0 && q.Resolution == "" && q.VideoCodec == "" && q.AudioCodec == "" {
		return MetadataFallbackQuery{}, false
	}
	return q, true
}

func metadataCategoryHint(query MetadataFallbackQuery) string {
	if query.IsTV {
		return "TV"
	}
	if query.Year > 0 {
		return "MOVIE"
	}
	return ""
}

func metadataWindow(query MetadataFallbackQuery) (*time.Time, *time.Time) {
	if query.PostedAt == nil || query.PostedAt.IsZero() {
		return nil, nil
	}
	from := query.PostedAt.Add(-10 * 24 * time.Hour).UTC()
	to := query.PostedAt.Add(3 * 24 * time.Hour).UTC()
	return &from, &to
}

func bestMetadataFallbackMatch(query MetadataFallbackQuery, entries []pgindex.PredbEntrySummary) (MetadataFallbackMatch, bool) {
	matches := rankedMetadataFallbackMatches(query, entries, 1)
	if len(matches) == 0 {
		return MetadataFallbackMatch{}, false
	}
	return matches[0], true
}

func rankedMetadataFallbackMatches(query MetadataFallbackQuery, entries []pgindex.PredbEntrySummary, limit int) []MetadataFallbackMatch {
	if limit <= 0 {
		limit = len(entries)
	}
	out := make([]MetadataFallbackMatch, 0, len(entries))
	for _, entry := range entries {
		score := scoreMetadataFallback(query, entry)
		if score <= 0 {
			continue
		}
		out = append(out, MetadataFallbackMatch{Entry: entry, Confidence: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		di, iok := metadataPostedDelta(query, out[i].Entry)
		dj, jok := metadataPostedDelta(query, out[j].Entry)
		if iok && jok && di != dj {
			return di < dj
		}
		if iok != jok {
			return iok
		}
		return out[i].Entry.Title < out[j].Entry.Title
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func metadataFallbackAutoApplyAllowed(query MetadataFallbackQuery, match MetadataFallbackMatch) bool {
	if match.Confidence < 0.90 {
		return false
	}
	if delta, ok := metadataPostedDelta(query, match.Entry); ok {
		return delta <= metadataFallbackAutoApplyWindow
	}
	return false
}

func metadataPostedDelta(query MetadataFallbackQuery, entry pgindex.PredbEntrySummary) (time.Duration, bool) {
	if query.PostedAt == nil || entry.PostedAt == nil {
		return 0, false
	}
	diff := query.PostedAt.Sub(*entry.PostedAt)
	if diff < 0 {
		diff = -diff
	}
	return diff, true
}

func scoreMetadataFallback(query MetadataFallbackQuery, entry pgindex.PredbEntrySummary) float64 {
	score := 0.0
	titleLower := strings.ToLower(entry.Title)
	if query.IsTV {
		if query.Season > 0 && query.Episode > 0 {
			needle := fmt.Sprintf("s%02de%02d", query.Season, query.Episode)
			if strings.Contains(titleLower, needle) {
				score += 0.50
			} else {
				return 0
			}
		}
		if strings.HasPrefix(strings.ToUpper(entry.Category), "TV") {
			score += 0.15
		}
	}
	if query.Year > 0 {
		if year := extractYear(entry.Title); year > 0 {
			switch absInt(query.Year - year) {
			case 0:
				score += 0.12
			case 1:
				score += 0.05
			default:
				score -= 0.05
			}
		}
	}
	if query.Resolution != "" && strings.Contains(strings.ToLower(entry.Title+" "+entry.Category), strings.ToLower(query.Resolution)) {
		score += 0.10
	}
	if query.VideoCodec != "" && strings.Contains(strings.ToLower(entry.Title), strings.ToLower(query.VideoCodec)) {
		score += 0.06
	}
	if query.AudioCodec != "" && strings.Contains(strings.ToLower(entry.Title), strings.ToLower(query.AudioCodec)) {
		score += 0.04
	}
	if query.PayloadSize > 0 && entry.SizeKB > 0 {
		if ratio, ok := bestPredbSizeRatio(query.PayloadSize, entry.SizeKB); ok {
			switch {
			case ratio <= 0.08:
				score += 0.55
			case ratio <= 0.15:
				score += 0.35
			case ratio <= 0.25:
				score += 0.18
			case ratio > 0.50:
				score -= 0.10
			}
		}
	}
	if query.PostedAt != nil && entry.PostedAt != nil {
		diff := query.PostedAt.Sub(*entry.PostedAt)
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff <= 24*time.Hour:
			score += 0.22
		case diff <= 72*time.Hour:
			score += 0.15
		case diff <= 10*24*time.Hour:
			score += 0.06
		default:
			score -= 0.08
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

func bestPredbSizeRatio(payloadBytes int64, storedSizeKB float64) (float64, bool) {
	if payloadBytes <= 0 || storedSizeKB <= 0 {
		return 0, false
	}
	candidates := []int64{
		int64(storedSizeKB * 1024),
		int64(storedSizeKB * 1024 * 1024),
	}
	best := 0.0
	seen := false
	for _, entryBytes := range candidates {
		if entryBytes <= 0 {
			continue
		}
		ratio := float64(absInt64(payloadBytes-entryBytes)) / float64(maxInt64(payloadBytes, entryBytes))
		if !seen || ratio < best {
			best = ratio
			seen = true
		}
	}
	return best, seen
}
