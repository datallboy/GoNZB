package release

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const releaseJoinThreshold = 0.55

type releaseCluster struct {
	Binaries        []pgindex.BinarySummary
	MatchConfidence float64
}

type clusterScore struct {
	value float64
}

var (
	multiSpaceRE       = regexp.MustCompile(`\s+`)
	separatorRE        = regexp.MustCompile(`[._\-]+`)
	resolutionRE       = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p)\b`)
	videoCodecRE       = regexp.MustCompile(`(?i)\b(x265|h265|hevc|av1|x264|h264|xvid)\b`)
	audioCodecRE       = regexp.MustCompile(`(?i)\b(truehd|atmos|dts[- ]?hd|dts|ddp|eac3|ac3|aac|flac|mp3)\b`)
	sourceTagRE        = regexp.MustCompile(`(?i)\b(remux|bluray|bdrip|webrip|web[- ]?dl|hdtv|dvdrip|cam)\b`)
	subtitleLanguageRE = regexp.MustCompile(`(?i)\b(eng|english|spa|spanish|fre|french|ger|german|ita|italian|jpn|japanese)\b`)
	rarPartRE          = regexp.MustCompile(`(?i)\.part\d+\.rar$|\.r\d{2,3}$`)
	parVolumeRE        = regexp.MustCompile(`(?i)\.vol\d+\+\d+\.par2$`)
	numericNoiseOnlyRE = regexp.MustCompile(`^[a-f0-9]{8,}$`)
)

func clusterBinaries(candidate pgindex.ReleaseCandidate, binaries []pgindex.BinarySummary) []releaseCluster {
	ordered := append([]pgindex.BinarySummary(nil), binaries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftTime := binarySortTime(ordered[i].PostedAt)
		rightTime := binarySortTime(ordered[j].PostedAt)
		if !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		if ordered[i].FirstArticleNumber != ordered[j].FirstArticleNumber {
			return ordered[i].FirstArticleNumber < ordered[j].FirstArticleNumber
		}
		return ordered[i].BinaryID < ordered[j].BinaryID
	})

	clusters := make([]releaseCluster, 0, len(ordered))
	for _, binary := range ordered {
		bestIdx := -1
		bestScore := 0.0

		for idx := range clusters {
			score := scoreBinaryAgainstCluster(candidate, binary, clusters[idx])
			if score > bestScore {
				bestScore = score
				bestIdx = idx
			}
		}

		if bestIdx >= 0 && bestScore >= releaseJoinThreshold {
			clusters[bestIdx].Binaries = append(clusters[bestIdx].Binaries, binary)
			continue
		}

		clusters = append(clusters, releaseCluster{
			Binaries: []pgindex.BinarySummary{binary},
		})
	}

	for idx := range clusters {
		sort.SliceStable(clusters[idx].Binaries, func(i, j int) bool {
			left := strings.ToLower(pickFileName(clusters[idx].Binaries[i]))
			right := strings.ToLower(pickFileName(clusters[idx].Binaries[j]))
			if left == right {
				return clusters[idx].Binaries[i].BinaryID < clusters[idx].Binaries[j].BinaryID
			}
			return left < right
		})
		clusters[idx].MatchConfidence = scoreCluster(candidate, clusters[idx])
	}

	return clusters
}

func scoreBinaryAgainstCluster(candidate pgindex.ReleaseCandidate, binary pgindex.BinarySummary, cluster releaseCluster) float64 {
	score := 0.22

	if dominant := dominantPoster(cluster.Binaries); dominant != "" && strings.EqualFold(strings.TrimSpace(binary.Poster), dominant) {
		score += 0.16
	}

	timeDelta := clusterTimeDelta(binary, cluster.Binaries)
	switch {
	case timeDelta >= 0 && timeDelta <= 2*time.Hour:
		score += 0.16
	case timeDelta <= 12*time.Hour:
		score += 0.10
	case timeDelta <= 24*time.Hour:
		score += 0.05
	}

	if stemsRelated(bestBinaryStem(binary), representativeStem(cluster.Binaries)) {
		score += 0.16
	}

	if titlesLookRelated(bestBinaryTitle(candidate, binary), representativeTitle(candidate, cluster.Binaries)) {
		score += 0.08
	}

	if clusterHasComplementaryFiles(binary, cluster.Binaries) {
		score += 0.07
	}

	if sizeLooksCoherent(binary, cluster.Binaries) {
		score += 0.05
	}

	score += clamp01(binary.MatchConfidence) * 0.10
	return clamp01(score)
}

func scoreCluster(candidate pgindex.ReleaseCandidate, cluster releaseCluster) float64 {
	if len(cluster.Binaries) == 0 {
		return 0
	}

	score := 0.20
	score += averageBinaryMatch(cluster.Binaries) * 0.30

	if len(cluster.Binaries) > 1 {
		if dominantPosterRatio(cluster.Binaries) >= 0.75 {
			score += 0.14
		}

		span := clusterPostingSpan(cluster.Binaries)
		switch {
		case span >= 0 && span <= 2*time.Hour:
			score += 0.14
		case span <= 12*time.Hour:
			score += 0.09
		case span <= 24*time.Hour:
			score += 0.04
		}
	}

	if hasPARRelation(cluster.Binaries) {
		score += 0.10
	}
	if hasArchiveOrMediaMix(cluster.Binaries) {
		score += 0.06
	}
	if titlesLookRelated(representativeTitle(candidate, cluster.Binaries), candidate.ReleaseName) {
		score += 0.04
	}

	completion := clusterCompletionPct(cluster.Binaries)
	if completion >= 95 {
		score += 0.06
	} else if completion >= 75 {
		score += 0.04
	} else if completion >= 50 {
		score += 0.02
	}

	return clamp01(score)
}

func buildReleaseRecord(candidate pgindex.ReleaseCandidate, cluster releaseCluster) pgindex.ReleaseRecord {
	sourceTitle := representativeTitle(candidate, cluster.Binaries)
	deobfuscatedTitle := deobfuscateTitle(sourceTitle, cluster.Binaries)
	if deobfuscatedTitle == "" {
		deobfuscatedTitle = sourceTitle
	}

	identityScore := computeIdentityConfidenceScore(cluster)
	identityStatus := classifyIdentityStatus(identityScore)
	finalTitle := strings.TrimSpace(deobfuscatedTitle)
	if finalTitle == "" {
		finalTitle = strings.TrimSpace(sourceTitle)
	}
	if finalTitle == "" {
		finalTitle = "unknown-release"
	}

	primaryResolution, mediaTags := detectPrimaryResolution(cluster.Binaries)
	primaryVideoCodec := detectPrimaryVideoCodec(cluster.Binaries)
	primaryAudioCodec := detectPrimaryAudioCodec(cluster.Binaries)
	availabilityScore := computeAvailabilityScore(cluster)
	mediaQualityScore := computeMediaQualityScore(primaryResolution, primaryVideoCodec, cluster.Binaries)
	passworded := false
	passwordedKnown := false
	passwordedUnknown := false
	passwordState := derivePasswordState(passworded, passwordedKnown, passwordedUnknown)
	hasPAR2, hasNFO, archiveCount, videoCount, audioCount, samplePresent := summarizeFiles(cluster.Binaries)
	classification := classifyCluster(cluster.Binaries, archiveCount, videoCount, audioCount)
	subtitles := detectSubtitleLanguages(cluster.Binaries)
	postedAt := earliestPostedAt(candidate.PostedAt, cluster.Binaries)
	now := time.Now().UTC()

	return pgindex.ReleaseRecord{
		ProviderID:              candidate.ProviderID,
		ReleaseKey:              candidate.ReleaseKey,
		GroupName:               deriveGroupName(candidate, cluster.Binaries),
		Title:                   finalTitle,
		SourceTitle:             sourceTitle,
		DeobfuscatedTitle:       deobfuscatedTitle,
		SearchTitle:             normalizeSearchTitle(finalTitle),
		Category:                "usenet",
		Classification:          classification,
		Poster:                  dominantPoster(cluster.Binaries),
		SizeBytes:               totalBytes(cluster.Binaries),
		PostedAt:                postedAt,
		FileCount:               len(cluster.Binaries),
		ParFileCount:            countPARFiles(cluster.Binaries),
		CompletionPct:           clusterCompletionPct(cluster.Binaries),
		MatchConfidence:         clamp01(cluster.MatchConfidence),
		IdentityStatus:          identityStatus,
		Passworded:              passworded,
		PasswordedKnown:         passwordedKnown,
		PasswordedUnknown:       passwordedUnknown,
		PasswordState:           passwordState,
		Encrypted:               false,
		HasPAR2:                 hasPAR2,
		HasNFO:                  hasNFO,
		ArchiveCount:            archiveCount,
		VideoCount:              videoCount,
		AudioCount:              audioCount,
		SamplePresent:           samplePresent,
		AvailabilityScore:       availabilityScore,
		AvailabilityTier:        scoreTier(availabilityScore),
		MediaQualityScore:       mediaQualityScore,
		MediaQualityTier:        mediaQualityTier(mediaQualityScore),
		IdentityConfidenceScore: identityScore,
		RuntimeSeconds:          0,
		PrimaryResolution:       primaryResolution,
		PrimaryVideoCodec:       primaryVideoCodec,
		PrimaryAudioCodec:       primaryAudioCodec,
		SubtitleLanguages:       subtitles,
		MediaTags:               mediaTags,
		MetadataUpdatedAt:       &now,
	}
}

func deriveGroupName(candidate pgindex.ReleaseCandidate, binaries []pgindex.BinarySummary) string {
	seed := strings.Join([]string{
		candidate.ReleaseKey,
		normalizeSearchTitle(dominantPoster(binaries)),
		representativeStem(binaries),
		clusterTimeBucket(binaries),
	}, "|")
	guid := pgindex.StableReleaseGUID(candidate.ProviderID, seed)
	return "release-group-" + guid[:24]
}

func dominantPoster(binaries []pgindex.BinarySummary) string {
	counts := make(map[string]int)
	best := ""
	bestCount := 0
	for _, binary := range binaries {
		poster := strings.TrimSpace(binary.Poster)
		if poster == "" {
			continue
		}
		counts[poster]++
		if counts[poster] > bestCount {
			best = poster
			bestCount = counts[poster]
		}
	}
	return best
}

func dominantPosterRatio(binaries []pgindex.BinarySummary) float64 {
	if len(binaries) == 0 {
		return 0
	}
	dominant := dominantPoster(binaries)
	if dominant == "" {
		return 0
	}
	count := 0
	for _, binary := range binaries {
		if strings.EqualFold(strings.TrimSpace(binary.Poster), dominant) {
			count++
		}
	}
	return float64(count) / float64(len(binaries))
}

func clusterPostingSpan(binaries []pgindex.BinarySummary) time.Duration {
	if len(binaries) <= 1 {
		return 0
	}

	var minTime, maxTime *time.Time
	for _, binary := range binaries {
		if binary.PostedAt == nil {
			continue
		}
		t := binary.PostedAt.UTC()
		if minTime == nil || t.Before(*minTime) {
			tmp := t
			minTime = &tmp
		}
		if maxTime == nil || t.After(*maxTime) {
			tmp := t
			maxTime = &tmp
		}
	}
	if minTime == nil || maxTime == nil {
		return 0
	}
	return maxTime.Sub(*minTime)
}

func clusterTimeDelta(binary pgindex.BinarySummary, binaries []pgindex.BinarySummary) time.Duration {
	if binary.PostedAt == nil {
		return 365 * 24 * time.Hour
	}
	best := 365 * 24 * time.Hour
	for _, item := range binaries {
		if item.PostedAt == nil {
			continue
		}
		delta := item.PostedAt.UTC().Sub(binary.PostedAt.UTC())
		if delta < 0 {
			delta = -delta
		}
		if delta < best {
			best = delta
		}
	}
	return best
}

func clusterTimeBucket(binaries []pgindex.BinarySummary) string {
	postedAt := earliestPostedAt(nil, binaries)
	if postedAt == nil {
		return "no-time"
	}
	utc := postedAt.UTC()
	return fmt.Sprintf("%s-%d", utc.Format("20060102"), utc.Hour()/6)
}

func representativeTitle(candidate pgindex.ReleaseCandidate, binaries []pgindex.BinarySummary) string {
	title := strings.TrimSpace(candidate.ReleaseName)
	if title != "" {
		return title
	}
	for _, binary := range binaries {
		if value := strings.TrimSpace(binary.ReleaseName); value != "" {
			return value
		}
	}
	if stem := representativeStem(binaries); stem != "" {
		return humanizeTitle(stem)
	}
	if value := strings.TrimSpace(candidate.ReleaseKey); value != "" {
		return humanizeTitle(value)
	}
	return "unknown-release"
}

func representativeStem(binaries []pgindex.BinarySummary) string {
	for _, binary := range binaries {
		if isAuxiliaryFile(pickFileName(binary)) {
			continue
		}
		if stem := bestBinaryStem(binary); stem != "" {
			return stem
		}
	}
	for _, binary := range binaries {
		if stem := bestBinaryStem(binary); stem != "" {
			return stem
		}
	}
	return ""
}

func bestBinaryTitle(candidate pgindex.ReleaseCandidate, binary pgindex.BinarySummary) string {
	if value := strings.TrimSpace(binary.ReleaseName); value != "" {
		return value
	}
	if value := strings.TrimSpace(candidate.ReleaseName); value != "" {
		return value
	}
	if stem := bestBinaryStem(binary); stem != "" {
		return humanizeTitle(stem)
	}
	return ""
}

func bestBinaryStem(binary pgindex.BinarySummary) string {
	name := pickFileName(binary)
	if name == "" {
		return ""
	}
	return normalizeStem(name)
}

func normalizeStem(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return ""
	}
	switch {
	case parVolumeRE.MatchString(lower):
		lower = parVolumeRE.ReplaceAllString(lower, "")
	case strings.HasSuffix(lower, ".par2"):
		lower = strings.TrimSuffix(lower, ".par2")
	case rarPartRE.MatchString(lower):
		lower = rarPartRE.ReplaceAllString(lower, "")
	default:
		lower = strings.TrimSuffix(lower, filepath.Ext(lower))
	}
	lower = separatorRE.ReplaceAllString(lower, " ")
	lower = multiSpaceRE.ReplaceAllString(lower, " ")
	return strings.TrimSpace(lower)
}

func stemsRelated(a, b string) bool {
	a = normalizeSearchTitle(a)
	b = normalizeSearchTitle(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func titlesLookRelated(a, b string) bool {
	a = normalizeSearchTitle(a)
	b = normalizeSearchTitle(b)
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

func clusterHasComplementaryFiles(binary pgindex.BinarySummary, binaries []pgindex.BinarySummary) bool {
	name := strings.ToLower(pickFileName(binary))
	if name == "" {
		return false
	}
	if isParFile(name) {
		for _, other := range binaries {
			if !isParFile(pickFileName(other)) {
				return true
			}
		}
		return false
	}
	if strings.HasSuffix(name, ".nfo") {
		for _, other := range binaries {
			if stemsRelated(bestBinaryStem(binary), bestBinaryStem(other)) {
				return true
			}
		}
		return false
	}
	for _, other := range binaries {
		otherName := strings.ToLower(pickFileName(other))
		if otherName == "" {
			continue
		}
		if isParFile(otherName) || strings.HasSuffix(otherName, ".nfo") || isArchiveFile(otherName) {
			return true
		}
	}
	return false
}

func sizeLooksCoherent(binary pgindex.BinarySummary, binaries []pgindex.BinarySummary) bool {
	if binary.TotalBytes <= 0 || len(binaries) == 0 {
		return false
	}
	for _, other := range binaries {
		if other.TotalBytes <= 0 {
			continue
		}
		if other.TotalBytes == binary.TotalBytes {
			return true
		}
		diff := other.TotalBytes - binary.TotalBytes
		if diff < 0 {
			diff = -diff
		}
		larger := other.TotalBytes
		if binary.TotalBytes > larger {
			larger = binary.TotalBytes
		}
		if larger > 0 && float64(diff)/float64(larger) <= 0.15 {
			return true
		}
	}
	return false
}

func averageBinaryMatch(binaries []pgindex.BinarySummary) float64 {
	if len(binaries) == 0 {
		return 0
	}
	total := 0.0
	for _, binary := range binaries {
		total += clamp01(binary.MatchConfidence)
	}
	return total / float64(len(binaries))
}

func hasPARRelation(binaries []pgindex.BinarySummary) bool {
	hasPAR := false
	hasMain := false
	base := representativeStem(binaries)
	for _, binary := range binaries {
		name := pickFileName(binary)
		if isParFile(name) && stemsRelated(bestBinaryStem(binary), base) {
			hasPAR = true
		}
		if !isParFile(name) && !isAuxiliaryFile(name) {
			hasMain = true
		}
	}
	return hasPAR && hasMain
}

func hasArchiveOrMediaMix(binaries []pgindex.BinarySummary) bool {
	archiveCount := 0
	mediaCount := 0
	for _, binary := range binaries {
		name := strings.ToLower(pickFileName(binary))
		switch {
		case isArchiveFile(name):
			archiveCount++
		case isVideoFile(name), isAudioFile(name):
			mediaCount++
		}
	}
	return archiveCount > 0 || mediaCount > 0
}

func clusterCompletionPct(binaries []pgindex.BinarySummary) float64 {
	totalObservedParts := 0
	totalExpectedParts := 0
	for _, binary := range binaries {
		totalObservedParts += binary.ObservedParts
		totalExpectedParts += max(binary.TotalParts, binary.ObservedParts)
	}
	if totalExpectedParts <= 0 {
		return 100
	}
	pct := (float64(totalObservedParts) / float64(totalExpectedParts)) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

func computeIdentityConfidenceScore(cluster releaseCluster) float64 {
	score := (cluster.MatchConfidence * 100 * 0.6) + (averageBinaryMatch(cluster.Binaries) * 100 * 0.4)
	return clampScore(score)
}

func classifyIdentityStatus(score float64) string {
	switch {
	case score >= 75:
		return "identified"
	case score >= 55:
		return "probable"
	default:
		return "unknown"
	}
}

func computeAvailabilityScore(cluster releaseCluster) float64 {
	completion := clusterCompletionPct(cluster.Binaries)
	score := completion * 0.72
	if countPARFiles(cluster.Binaries) > 0 {
		score += 12
	}
	if len(cluster.Binaries) >= 2 {
		score += 8
	}
	score += averageBinaryMatch(cluster.Binaries) * 8
	return clampScore(score)
}

func scoreTier(score float64) string {
	switch {
	case score >= 85:
		return "excellent"
	case score >= 70:
		return "good"
	case score >= 50:
		return "partial"
	default:
		return "low"
	}
}

func mediaQualityTier(score float64) string {
	switch {
	case score >= 85:
		return "premium"
	case score >= 65:
		return "good"
	case score >= 45:
		return "standard"
	default:
		return "unknown"
	}
}

func computeMediaQualityScore(primaryResolution, primaryVideoCodec string, binaries []pgindex.BinarySummary) float64 {
	score := 25.0
	switch strings.ToLower(primaryResolution) {
	case "2160p":
		score += 35
	case "1080p":
		score += 28
	case "720p":
		score += 20
	case "576p", "480p":
		score += 10
	}

	sourceTag := detectPrimarySourceTag(binaries)
	switch sourceTag {
	case "remux":
		score += 25
	case "bluray", "bdrip":
		score += 20
	case "webrip", "web-dl":
		score += 16
	case "hdtv":
		score += 10
	case "dvdrip":
		score += 8
	case "cam":
		score -= 10
	}

	switch strings.ToLower(primaryVideoCodec) {
	case "hevc", "x265", "h265", "av1":
		score += 12
	case "x264", "h264":
		score += 8
	case "xvid":
		score += 2
	}

	return clampScore(score)
}

func summarizeFiles(binaries []pgindex.BinarySummary) (hasPAR2, hasNFO bool, archiveCount, videoCount, audioCount int, samplePresent bool) {
	for _, binary := range binaries {
		name := strings.ToLower(pickFileName(binary))
		switch {
		case isParFile(name):
			hasPAR2 = true
		case strings.HasSuffix(name, ".nfo"):
			hasNFO = true
		case isArchiveFile(name):
			archiveCount++
		case isVideoFile(name):
			videoCount++
		case isAudioFile(name):
			audioCount++
		}
		if strings.Contains(name, "sample") {
			samplePresent = true
		}
	}
	return hasPAR2, hasNFO, archiveCount, videoCount, audioCount, samplePresent
}

func classifyCluster(binaries []pgindex.BinarySummary, archiveCount, videoCount, audioCount int) string {
	switch {
	case videoCount > 0 && archiveCount > 0:
		return "video_archive"
	case videoCount > 0:
		return "video"
	case audioCount > 0:
		return "audio"
	case archiveCount > 0:
		return "archive"
	case countPARFiles(binaries) > 0:
		return "repair_set"
	default:
		return "misc"
	}
}

func detectPrimaryResolution(binaries []pgindex.BinarySummary) (string, []string) {
	tags := make([]string, 0, 8)
	for _, binary := range binaries {
		text := strings.ToLower(bestBinaryTitle(pgindex.ReleaseCandidate{}, binary) + " " + pickFileName(binary))
		if match := resolutionRE.FindString(text); match != "" {
			tags = append(tags, strings.ToLower(match))
		}
		if match := sourceTagRE.FindString(text); match != "" {
			tags = append(tags, normalizeTag(match))
		}
	}
	tags = uniqueSortedStrings(tags)
	if len(tags) == 0 {
		return "", tags
	}
	for _, tag := range tags {
		switch tag {
		case "2160p", "1080p", "720p", "576p", "480p":
			return tag, tags
		}
	}
	return "", tags
}

func detectPrimaryVideoCodec(binaries []pgindex.BinarySummary) string {
	for _, binary := range binaries {
		text := strings.ToLower(bestBinaryTitle(pgindex.ReleaseCandidate{}, binary) + " " + pickFileName(binary))
		if match := videoCodecRE.FindString(text); match != "" {
			return normalizeTag(match)
		}
	}
	return ""
}

func detectPrimaryAudioCodec(binaries []pgindex.BinarySummary) string {
	for _, binary := range binaries {
		text := strings.ToLower(bestBinaryTitle(pgindex.ReleaseCandidate{}, binary) + " " + pickFileName(binary))
		if match := audioCodecRE.FindString(text); match != "" {
			return normalizeTag(match)
		}
	}
	return ""
}

func detectPrimarySourceTag(binaries []pgindex.BinarySummary) string {
	for _, binary := range binaries {
		text := strings.ToLower(bestBinaryTitle(pgindex.ReleaseCandidate{}, binary) + " " + pickFileName(binary))
		if match := sourceTagRE.FindString(text); match != "" {
			return normalizeTag(match)
		}
	}
	return ""
}

func detectSubtitleLanguages(binaries []pgindex.BinarySummary) []string {
	languages := make([]string, 0, 4)
	for _, binary := range binaries {
		name := strings.ToLower(pickFileName(binary))
		if !strings.HasSuffix(name, ".srt") && !strings.HasSuffix(name, ".sub") && !strings.HasSuffix(name, ".ass") {
			continue
		}
		if match := subtitleLanguageRE.FindString(name); match != "" {
			languages = append(languages, normalizeLanguage(match))
		}
	}
	return uniqueSortedStrings(languages)
}

func derivePasswordState(passworded, known, unknown bool) string {
	switch {
	case known:
		return "passworded_known"
	case unknown:
		return "passworded_unknown"
	case passworded:
		return "passworded"
	default:
		return "not_passworded"
	}
}

func deobfuscateTitle(sourceTitle string, binaries []pgindex.BinarySummary) string {
	sourceTitle = strings.TrimSpace(sourceTitle)
	if sourceTitle == "" {
		if stem := representativeStem(binaries); stem != "" {
			return humanizeTitle(stem)
		}
		return ""
	}

	normalized := normalizeSearchTitle(sourceTitle)
	if normalized == "" || numericNoiseOnlyRE.MatchString(strings.ReplaceAll(normalized, " ", "")) {
		if stem := representativeStem(binaries); stem != "" {
			return humanizeTitle(stem)
		}
	}

	return humanizeTitle(sourceTitle)
}

func pickFileName(binary pgindex.BinarySummary) string {
	name := strings.TrimSpace(binary.FileName)
	if name != "" {
		return name
	}

	name = strings.TrimSpace(binary.BinaryName)
	if name != "" {
		return name
	}

	name = strings.TrimSpace(binary.ReleaseName)
	if name == "" {
		name = strings.TrimSpace(binary.ReleaseKey)
	}
	if name == "" {
		name = fmt.Sprintf("binary-%d.bin", binary.BinaryID)
	}

	if filepath.Ext(name) == "" {
		name += ".bin"
	}
	return name
}

func isParFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".par2") || strings.Contains(lower, ".vol")
}

func isArchiveFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".rar") ||
		strings.HasSuffix(lower, ".zip") ||
		strings.HasSuffix(lower, ".7z") ||
		rarPartRE.MatchString(lower)
}

func isVideoFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".mkv") ||
		strings.HasSuffix(lower, ".mp4") ||
		strings.HasSuffix(lower, ".avi") ||
		strings.HasSuffix(lower, ".ts")
}

func isAudioFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".flac") ||
		strings.HasSuffix(lower, ".mp3") ||
		strings.HasSuffix(lower, ".m4a")
}

func isAuxiliaryFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return isParFile(lower) ||
		strings.HasSuffix(lower, ".nfo") ||
		strings.HasSuffix(lower, ".sfv") ||
		strings.HasSuffix(lower, ".srr")
}

func earliestPostedAt(candidate *time.Time, binaries []pgindex.BinarySummary) *time.Time {
	var best *time.Time

	if candidate != nil {
		t := candidate.UTC()
		best = &t
	}

	for _, binary := range binaries {
		if binary.PostedAt == nil {
			continue
		}
		t := binary.PostedAt.UTC()
		if best == nil || t.Before(*best) {
			best = &t
		}
	}

	return best
}

func totalBytes(binaries []pgindex.BinarySummary) int64 {
	total := int64(0)
	for _, binary := range binaries {
		total += binary.TotalBytes
	}
	return total
}

func countPARFiles(binaries []pgindex.BinarySummary) int {
	count := 0
	for _, binary := range binaries {
		if isParFile(pickFileName(binary)) {
			count++
		}
	}
	return count
}

func normalizeSearchTitle(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", " ")
	v = strings.ReplaceAll(v, ".", " ")
	v = strings.ReplaceAll(v, "-", " ")
	v = strings.Join(strings.Fields(v), " ")
	return v
}

func humanizeTitle(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = separatorRE.ReplaceAllString(v, " ")
	v = multiSpaceRE.ReplaceAllString(v, " ")
	return strings.TrimSpace(v)
}

func normalizeTag(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, " ", "")
	switch v {
	case "webdl", "web-dl":
		return "web-dl"
	case "dtshd":
		return "dts-hd"
	default:
		return v
	}
}

func normalizeLanguage(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "eng", "english":
		return "english"
	case "spa", "spanish":
		return "spanish"
	case "fre", "french":
		return "french"
	case "ger", "german":
		return "german"
	case "ita", "italian":
		return "italian"
	case "jpn", "japanese":
		return "japanese"
	default:
		return v
	}
}

func uniqueSortedStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func clampScore(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}

func binarySortTime(t *time.Time) time.Time {
	if t == nil || t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t.UTC()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
