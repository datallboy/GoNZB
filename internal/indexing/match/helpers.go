package match

import (
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type structuredData struct {
	Name      string
	Part      int
	Total     int
	Size      int64
	FileIndex int
	FileTotal int
}

type counterPair struct {
	Part  int
	Total int
}

type matchState struct {
	candidate           Candidate
	opts                Options
	cleanSubject        string
	subjectWithoutYEnc  string
	normalizedSubject   string
	quotedFilename      string
	structured          structuredData
	fileIndex           int
	expectedFileCount   int
	partNumber          int
	totalParts          int
	releaseName         string
	fileName            string
	confidence          float64
	evidence            map[string]any
	shortCircuitedAfter string
	fallbackUsed        bool
}

var (
	quotedFilenameRE = regexp.MustCompile(`"([^"]+)"`)
	partMarkerRE     = regexp.MustCompile(`(?i)(?:\(|\[)\s*(\d{1,5})\s*/\s*(\d{1,5})\s*(?:\)|\])`)
	partLooseRE      = regexp.MustCompile(`(?i)\b(\d{1,5})\s*/\s*(\d{1,5})\b`)
	yencTailRE       = regexp.MustCompile(`(?i)\s+yenc.*$`)
	yencNameRE       = regexp.MustCompile(`(?i)\bname\s*[:=]\s*(?:"([^"]+)"|([^\s]+))`)
	yencPartRE       = regexp.MustCompile(`(?i)\bpart\s*[:=]\s*(\d{1,5})\b`)
	yencTotalRE      = regexp.MustCompile(`(?i)\btotal\s*[:=]\s*(\d{1,5})\b`)
	yencSizeRE       = regexp.MustCompile(`(?i)\bsize\s*[:=]\s*(\d{1,18})\b`)
	extensionHintRE  = regexp.MustCompile(`(?i)\.(par2|vol\d+\+\d+\.par2|nfo|sfv|srr|rar|r\d{2,3}|zip|7z|mkv|avi|mp4|mp3|flac)\b`)
	multiSpaceRE     = regexp.MustCompile(`\s+`)
	separatorRE      = regexp.MustCompile(`[\[\]\(\)\{\}\-_=+,;:]+`)
	unsafeFileRE     = regexp.MustCompile(`[\\/:*?"<>|]+`)
	nonKeyCharsRE    = regexp.MustCompile(`[^\pL\pN]+`)
	parFileRE        = regexp.MustCompile(`(?i)\.par2$|\.vol\d+\+\d+\.par2$`)
	parVolumeStemRE  = regexp.MustCompile(`(?i)\.vol\d+\+\d+\.par2$`)
	splitArchiveRE   = regexp.MustCompile(`(?i)\.(7z|zip)\.\d{3}$`)
	rarFamilyRE      = regexp.MustCompile(`(?i)\.part\d+\.rar$|\.r\d{2,3}$`)
	volumeTokenRE    = regexp.MustCompile(`(?i)^vol\d+$`)
	partTokenRE      = regexp.MustCompile(`(?i)^part\d+$`)
	rarTokenRE       = regexp.MustCompile(`(?i)^r\d{2,3}$`)
	opaqueTokenRE    = regexp.MustCompile(`(?i)^[a-z0-9]{12,}$`)
	numericSetRE     = regexp.MustCompile(`(?i)^[0-9]{5,}\s+[a-z](?:\s+[a-z]{2,4})?$`)
)

func newMatchState(candidate Candidate, opts Options) *matchState {
	clean := strings.TrimSpace(html.UnescapeString(candidate.Subject))
	partNumber, totalParts := parsePartInfo(clean)
	fileIndex, expectedFileCount := parseFileInfo(clean, partNumber, totalParts)
	structured := parseStructuredData(clean, candidate.RawOverview)
	if structured.FileIndex > 0 {
		fileIndex = structured.FileIndex
	}
	if structured.FileTotal > expectedFileCount {
		expectedFileCount = structured.FileTotal
	}
	if structured.Part > 0 {
		partNumber = structured.Part
	}
	if structured.Total > totalParts {
		totalParts = structured.Total
	}

	return &matchState{
		candidate:          candidate,
		opts:               opts,
		cleanSubject:       clean,
		subjectWithoutYEnc: stripYEnc(clean),
		normalizedSubject:  normalizeKey(clean),
		quotedFilename:     extractQuotedFilename(clean),
		structured:         structured,
		fileIndex:          fileIndex,
		expectedFileCount:  expectedFileCount,
		partNumber:         maxInt(partNumber, 1),
		totalParts:         maxInt(totalParts, 1),
		evidence:           make(map[string]any, 12),
	}
}

func (s *matchState) addEvidence(name string, delta float64, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["score"] = clampConfidence(delta)
	s.evidence[name] = payload
	s.confidence = clampConfidence(s.confidence + delta)
}

func (s *matchState) hasStableIdentity() bool {
	return strings.TrimSpace(s.releaseName) != "" && strings.TrimSpace(s.bestFileName()) != ""
}

func (s *matchState) bestFileName() string {
	if v := sanitizeFileName(s.fileName); v != "" {
		return v
	}
	if v := sanitizeFileName(s.quotedFilename); v != "" {
		return v
	}
	if v := sanitizeFileName(s.structured.Name); v != "" {
		return v
	}
	return ""
}

func (s *matchState) bestExtension() string {
	for _, value := range []string{s.fileName, s.quotedFilename, s.structured.Name, s.cleanSubject} {
		if ext := extractExtensionHint(value); ext != "" {
			return ext
		}
	}
	return ""
}

func (s *matchState) contextSeed() string {
	parts := make([]string, 0, 6)

	if v := normalizePoster(s.candidate.Poster); v != "" {
		parts = append(parts, v)
	}
	if v := extractMessageHost(s.candidate.MessageID); v != "" {
		parts = append(parts, v)
	}
	if groups := parseXrefGroups(s.candidate.Xref); len(groups) > 0 {
		parts = append(parts, strings.Join(groups, "_"))
	}
	if v := derivePostingWindow(s.candidate.PostedAt); v != "" {
		parts = append(parts, v)
	}
	if ext := strings.TrimPrefix(strings.ToLower(s.bestExtension()), "."); ext != "" {
		parts = append(parts, ext)
	}
	if len(parts) == 0 {
		if v := normalizeKey(strings.Trim(s.candidate.MessageID, "<>")); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return "unknown-release"
	}
	return strings.Join(parts, "-")
}

func (s *matchState) releaseContextSeed() string {
	parts := make([]string, 0, 8)

	if v := normalizePoster(s.candidate.Poster); v != "" {
		parts = append(parts, v)
	}
	if v := extractMessageHost(s.candidate.MessageID); v != "" {
		parts = append(parts, v)
	}
	if groups := parseXrefGroups(s.candidate.Xref); len(groups) > 0 {
		parts = append(parts, strings.Join(groups, "_"))
	}
	if v := derivePostingWindow(s.candidate.PostedAt); v != "" {
		parts = append(parts, v)
	}
	if bucket := deriveArticleBucket(s.candidate.ArticleNumber, s.releaseArticleBucketSize()); bucket > 0 {
		parts = append(parts, fmt.Sprintf("release-%d", bucket))
	}
	if family := s.releaseFamilyHint(); family != "" {
		parts = append(parts, family)
	}
	if s.expectedFileCount > 0 {
		parts = append(parts, fmt.Sprintf("files-%d", s.expectedFileCount))
	}
	if len(parts) == 0 {
		return s.contextSeed()
	}
	return strings.Join(parts, "-")
}

func (s *matchState) releaseFamilyContextSeed() string {
	parts := make([]string, 0, 7)

	if v := normalizePoster(s.candidate.Poster); v != "" {
		parts = append(parts, v)
	}
	if groups := parseXrefGroups(s.candidate.Xref); len(groups) > 0 {
		parts = append(parts, strings.Join(groups, "_"))
	}
	if v := derivePostingWindow(s.candidate.PostedAt); v != "" {
		parts = append(parts, v)
	}
	if bucket := deriveArticleBucket(s.candidate.ArticleNumber, s.releaseArticleBucketSize()); bucket > 0 {
		parts = append(parts, fmt.Sprintf("release-%d", bucket))
	}
	if s.expectedFileCount > 0 {
		parts = append(parts, fmt.Sprintf("files-%d", s.expectedFileCount))
	}
	if len(parts) == 0 {
		return s.contextSeed()
	}
	return strings.Join(parts, "-")
}

func (s *matchState) releaseArticleBucketSize() int64 {
	if s.expectedFileCount <= 1 {
		return 100000
	}

	size := int64(s.expectedFileCount) * 300
	if size < 2500 {
		size = 2500
	}
	if size > 100000 {
		size = 100000
	}
	return size
}

func (s *matchState) releaseFamilyHint() string {
	for _, value := range []string{s.fileName, s.quotedFilename, s.structured.Name, s.cleanSubject} {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "" {
			continue
		}
		if match := splitArchiveRE.FindStringSubmatch(lower); len(match) == 2 {
			return strings.ToLower(match[1])
		}
		switch {
		case rarFamilyRE.MatchString(lower) || strings.HasSuffix(lower, ".rar"):
			return "rar"
		case parFileRE.MatchString(lower):
			return "par2"
		}
		if ext := strings.TrimPrefix(extractExtensionHint(lower), "."); ext != "" && !isAllDigits(ext) {
			return ext
		}
	}
	return ""
}

func (s *matchState) contextualReleaseName() string {
	if releaseName := deriveReleaseName(s.cleanSubject, s.bestFileName()); releaseName != "" {
		return releaseName
	}

	seed := sanitizeFileName(s.contextSeed())
	if seed == "" {
		return fallbackReleaseName(s.cleanSubject, s.candidate.MessageID)
	}

	return seed
}

func (s *matchState) contextualFileName() string {
	name := fallbackFileName(s.contextualReleaseName(), s.contextSeed())
	if ext := s.bestExtension(); ext != "" {
		if current := filepath.Ext(name); current == "" || strings.EqualFold(current, ".bin") {
			name = strings.TrimSuffix(name, current) + ext
		}
	}
	return sanitizeFileName(name)
}

func (s *matchState) finalize(opts Options) Result {
	releaseName := strings.TrimSpace(s.releaseName)
	if releaseName == "" {
		releaseName = s.contextualReleaseName()
		s.fallbackUsed = true
	}

	explicitFileName := s.bestFileName()
	fileName := explicitFileName
	if fileName == "" {
		fileName = s.contextualFileName()
		s.fallbackUsed = true
	}

	fileName = sanitizeFileName(fileName)
	if fileName == "" {
		fileName = fallbackFileName(releaseName, s.contextSeed())
		s.fallbackUsed = true
	}

	if s.totalParts < s.partNumber {
		s.totalParts = s.partNumber
	}
	if s.totalParts <= 0 {
		s.totalParts = 1
	}
	if s.partNumber <= 0 {
		s.partNumber = 1
	}

	subjectSetToken, subjectSetKind := subjectSetToken(s.cleanSubject, explicitFileName)
	sourceReleaseKey := s.sourceReleaseKey(releaseName, explicitFileName)
	releaseFamilyKey, familyKind, baseStem := s.releaseFamilyKey(releaseName, explicitFileName, subjectSetToken, subjectSetKind)
	if baseStem == "" {
		for _, value := range []string{explicitFileName, s.quotedFilename, s.structured.Name, s.fileName} {
			if stem := archiveFamilyBaseStem(value); stem != "" {
				baseStem = stem
				break
			}
		}
	}
	if releaseFamilyKey == "" {
		releaseFamilyKey = sourceReleaseKey
	}
	if sourceReleaseKey == "" {
		sourceReleaseKey = releaseFamilyKey
	}

	fileKey := normalizeKey(fileName)
	if fileKey == "" {
		fileKey = normalizeKey(sourceReleaseKey)
	}
	if explicitFileName == "" && s.fileIndex > 0 && s.expectedFileCount > 0 {
		fileKey = normalizeKey("file " + strconv.Itoa(s.fileIndex) + " of " + strconv.Itoa(s.expectedFileCount))
	}
	fileSetKey := s.fileSetKey(releaseFamilyKey, subjectSetToken)
	identityStrength, identityReason := identityClassification(familyKind, subjectSetKind, baseStem, explicitFileName, s.confidence)

	status := "low_confidence"
	if s.confidence >= opts.HighConfidenceThreshold {
		status = "matched"
	} else if s.confidence >= opts.ProbableConfidenceThreshold {
		status = "probable"
	}

	s.evidence["summary"] = map[string]any{
		"confidence":            clampConfidence(s.confidence),
		"status":                status,
		"fallback_used":         s.fallbackUsed,
		"file_index":            s.fileIndex,
		"expected_file_count":   s.expectedFileCount,
		"source_release_key":    sourceReleaseKey,
		"release_family_key":    releaseFamilyKey,
		"file_set_key":          fileSetKey,
		"identity_strength":     identityStrength,
		"identity_reason":       identityReason,
		"subject_set_token":     subjectSetToken,
		"subject_set_kind":      subjectSetKind,
		"family_kind":           familyKind,
		"base_stem":             baseStem,
		"short_circuited_after": s.shortCircuitedAfter,
	}
	if s.fallbackUsed {
		s.evidence["fallback"] = map[string]any{
			"context_seed": s.contextSeed(),
			"used":         true,
		}
	}

	isAuxiliary := isAuxiliaryFamilyFile(fileName)
	isMainPayload := fileName != "" && !isAuxiliary
	fileFamilyKey := normalizeKey(firstNonEmpty(baseStem, fileName, releaseFamilyKey))

	return Result{
		SourceReleaseKey:  sourceReleaseKey,
		ReleaseFamilyKey:  releaseFamilyKey,
		FileSetKey:        fileSetKey,
		FileFamilyKey:     fileFamilyKey,
		IdentityStrength:  identityStrength,
		IdentityReason:    identityReason,
		SubjectSetToken:   subjectSetToken,
		SubjectSetKind:    subjectSetKind,
		FamilyKind:        familyKind,
		BaseStem:          baseStem,
		IsAuxiliary:       isAuxiliary,
		IsMainPayload:     isMainPayload,
		ReleaseName:       releaseName,
		ReleaseKey:        releaseFamilyKey,
		BinaryName:        fileName,
		BinaryKey:         sourceReleaseKey + "::" + fileKey,
		FileName:          fileName,
		FileIndex:         s.fileIndex,
		ExpectedFileCount: s.expectedFileCount,
		PartNumber:        s.partNumber,
		TotalParts:        s.totalParts,
		IsPars:            parFileRE.MatchString(strings.ToLower(fileName)),
		MatchConfidence:   clampConfidence(s.confidence),
		MatchStatus:       status,
		GroupingEvidence:  s.evidence,
	}
}

func (s *matchState) sourceReleaseKey(releaseName, explicitFileName string) string {
	releaseKey := ""
	if key := s.smallIndexedArchiveStemReleaseKey(explicitFileName); key != "" {
		releaseKey = key
	}
	if s.shouldPreferContextualReleaseKey(releaseName, explicitFileName) {
		if releaseKey == "" {
			releaseKey = canonicalReleaseKey(s.releaseContextSeed())
		}
	}
	if releaseKey == "" {
		releaseKey = canonicalReleaseKey(releaseName)
	}
	if releaseKey == "" {
		releaseKey = canonicalReleaseKey(s.contextSeed())
	}
	return releaseKey
}

func (s *matchState) releaseFamilyKey(releaseName, explicitFileName, subjectSetToken, subjectSetKind string) (string, string, string) {
	if subjectSetKind == "readable_title" {
		return subjectSetToken, "readable_title", archiveFamilyBaseStem(explicitFileName)
	}
	if key := readableReleaseFamilyKey(releaseName); key != "" && !isNumericObfuscatedSetKey(key) {
		return key, "readable_title", archiveFamilyBaseStem(explicitFileName)
	}

	for _, value := range []string{explicitFileName, s.quotedFilename, s.structured.Name, s.fileName} {
		if key, baseStem := s.smallArchiveFamilyReleaseKey(value); key != "" {
			return key, "archive_stem", baseStem
		}
	}

	if subjectSetToken != "" {
		switch subjectSetKind {
		case "numeric_obfuscated_set":
			return subjectSetToken, "numeric_obfuscated_set", ""
		case "opaque_set":
			return subjectSetToken, "opaque_set", ""
		}
	}

	if key := canonicalReleaseKey(s.releaseFamilyContextSeed()); key != "" {
		return key, "contextual_obfuscated", ""
	}
	return canonicalReleaseKey(s.contextSeed()), "contextual_obfuscated", ""
}

func (s *matchState) fileSetKey(releaseFamilyKey, subjectSetToken string) string {
	if subjectSetToken != "" && s.expectedFileCount > 0 {
		return strings.TrimSpace(fmt.Sprintf("%s files %d", subjectSetToken, s.expectedFileCount))
	}
	if s.expectedFileCount > 0 && releaseFamilyKey != "" {
		return strings.TrimSpace(fmt.Sprintf("%s files %d", releaseFamilyKey, s.expectedFileCount))
	}
	return releaseFamilyKey
}

func subjectSetToken(subject, fileName string) (string, string) {
	base := stripYEnc(subject)
	if match := partMarkerRE.FindStringIndex(base); match != nil && match[0] > 0 {
		base = base[:match[0]]
	} else if match := partLooseRE.FindStringIndex(base); match != nil && match[0] > 0 {
		base = base[:match[0]]
	}
	for _, quoted := range quotedFilenameRE.FindAllStringSubmatch(base, -1) {
		if len(quoted) > 1 {
			base = strings.ReplaceAll(base, quoted[0], " ")
		}
	}
	if fileName != "" {
		base = strings.ReplaceAll(base, fileName, " ")
	}
	token := canonicalReleaseKey(base)
	if token == "" {
		token = normalizeKey(base)
	}
	if token == "" {
		return "", ""
	}
	if isNumericObfuscatedSetKey(token) {
		return token, "numeric_obfuscated_set"
	}
	fields := strings.Fields(token)
	if len(fields) > 0 {
		opaque := 0
		for _, field := range fields {
			if opaqueTokenRE.MatchString(field) {
				opaque++
			}
		}
		if opaque == len(fields) {
			return token, "opaque_set"
		}
	}
	if readableReleaseFamilyKey(token) != "" {
		return token, "readable_title"
	}
	return token, "opaque_set"
}

func identityClassification(familyKind, subjectSetKind, baseStem, explicitFileName string, confidence float64) (string, string) {
	switch familyKind {
	case "archive_stem":
		return "strong", "archive_stem"
	case "readable_title":
		if archiveFamilyBaseStem(explicitFileName) != "" || baseStem != "" {
			return "strong", "readable_archive_filename"
		}
		return "probable", "readable_title"
	case "numeric_obfuscated_set":
		return "provisional", "numeric_obfuscated_set"
	case "opaque_set":
		return "provisional", "opaque_subject_set"
	case "contextual_obfuscated":
		return "weak", "contextual_fallback"
	}
	if subjectSetKind != "" {
		return "provisional", subjectSetKind
	}
	if confidence >= 0.85 {
		return "probable", "matcher_confidence"
	}
	return "weak", "matcher_fallback"
}

func (s *matchState) smallArchiveFamilyReleaseKey(fileName string) (string, string) {
	fileName = sanitizeFileName(fileName)
	lower := strings.ToLower(fileName)
	if lower == "" {
		return "", ""
	}
	if !splitArchiveRE.MatchString(lower) && !rarFamilyRE.MatchString(lower) && !strings.HasSuffix(lower, ".rar") && !parFileRE.MatchString(lower) {
		return "", ""
	}
	if s.fileIndex > 0 && s.expectedFileCount > 16 && splitArchiveRE.MatchString(lower) {
		return "", ""
	}
	baseStem := archiveFamilyBaseStem(fileName)
	if baseStem == "" {
		return "", ""
	}
	key := normalizeKey(baseStem)
	if key == "" {
		return "", ""
	}
	return key, baseStem
}

func (s *matchState) smallIndexedArchiveStemReleaseKey(explicitFileName string) string {
	if s.fileIndex <= 0 || s.expectedFileCount <= 1 || s.expectedFileCount > 16 {
		return ""
	}
	explicitFileName = sanitizeFileName(explicitFileName)
	if explicitFileName == "" {
		return ""
	}

	lower := strings.ToLower(explicitFileName)
	if !splitArchiveRE.MatchString(lower) && !rarFamilyRE.MatchString(lower) && !strings.HasSuffix(lower, ".rar") {
		return ""
	}

	key := canonicalReleaseKey(explicitFileName)
	fields := strings.Fields(key)
	if len(fields) != 1 || !opaqueTokenRE.MatchString(fields[0]) {
		return ""
	}
	return key
}

func (s *matchState) shouldPreferContextualReleaseKey(releaseName, explicitFileName string) bool {
	if s.structured.Total > 1 && isOpaqueReleaseIdentityCandidate(releaseName, explicitFileName) {
		return true
	}
	if s.fileIndex <= 0 || s.expectedFileCount <= 1 {
		return false
	}

	candidate := firstNonEmpty(releaseName, explicitFileName, s.quotedFilename, s.structured.Name)
	if candidate == "" {
		return false
	}

	key := canonicalReleaseKey(candidate)
	if key == "" {
		key = normalizeKey(candidate)
	}
	fields := strings.Fields(key)
	if len(fields) == 0 || len(fields) > 2 {
		return false
	}
	for _, field := range fields {
		if !opaqueTokenRE.MatchString(field) {
			return false
		}
	}
	return true
}

func isOpaqueReleaseIdentityCandidate(values ...string) bool {
	found := false
	for _, value := range values {
		key := canonicalReleaseKey(value)
		if key == "" {
			continue
		}
		found = true
		fields := strings.Fields(key)
		if len(fields) == 0 || len(fields) > 2 {
			return false
		}
		for _, field := range fields {
			if !opaqueTokenRE.MatchString(field) {
				return false
			}
		}
	}
	return found
}

func canonicalReleaseKey(value string) string {
	key := normalizeKey(value)
	if key == "" {
		return ""
	}

	tokens := strings.Fields(key)
	for len(tokens) > 0 {
		if len(tokens) >= 2 && volumeTokenRE.MatchString(tokens[len(tokens)-2]) && isAllDigits(tokens[len(tokens)-1]) {
			tokens = tokens[:len(tokens)-2]
			continue
		}
		if len(tokens) >= 2 && isCanonicalReleaseSuffix(tokens[len(tokens)-2]) && isAllDigits(tokens[len(tokens)-1]) {
			tokens = tokens[:len(tokens)-2]
			continue
		}

		last := tokens[len(tokens)-1]
		switch {
		case isCanonicalReleaseSuffix(last):
			tokens = tokens[:len(tokens)-1]
			continue
		case partTokenRE.MatchString(last):
			tokens = tokens[:len(tokens)-1]
			continue
		}
		break
	}

	return strings.Join(tokens, " ")
}

func readableReleaseFamilyKey(releaseName string) string {
	key := canonicalReleaseKey(releaseName)
	if key == "" {
		return ""
	}
	if isNumericObfuscatedSetKey(key) {
		return ""
	}
	fields := strings.Fields(key)
	if len(fields) == 0 {
		return ""
	}
	opaque := 0
	for _, field := range fields {
		if opaqueTokenRE.MatchString(field) {
			opaque++
		}
	}
	if opaque == len(fields) {
		return ""
	}
	return key
}

func isNumericObfuscatedSetKey(key string) bool {
	return numericSetRE.MatchString(strings.ToLower(strings.TrimSpace(key)))
}

func archiveFamilyBaseStem(fileName string) string {
	fileName = sanitizeFileName(fileName)
	if fileName == "" {
		return ""
	}

	lower := strings.ToLower(fileName)
	switch {
	case splitArchiveRE.MatchString(lower):
		lower = splitArchiveRE.ReplaceAllString(lower, "")
	case rarFamilyRE.MatchString(lower):
		lower = rarFamilyRE.ReplaceAllString(lower, "")
	case strings.HasSuffix(lower, ".rar"):
		lower = strings.TrimSuffix(lower, ".rar")
	case parFileRE.MatchString(lower):
		lower = parVolumeStemRE.ReplaceAllString(lower, "")
		lower = strings.TrimSuffix(lower, ".par2")
	default:
		lower = strings.TrimSuffix(lower, filepath.Ext(lower))
	}

	lower = separatorRE.ReplaceAllString(lower, " ")
	lower = multiSpaceRE.ReplaceAllString(lower, " ")
	lower = strings.TrimSpace(lower)
	if lower == "" {
		return ""
	}
	return lower
}

func isAuxiliaryFamilyFile(fileName string) bool {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if lower == "" {
		return false
	}
	return parFileRE.MatchString(lower) ||
		strings.HasSuffix(lower, ".nfo") ||
		strings.HasSuffix(lower, ".sfv") ||
		strings.HasSuffix(lower, ".srr") ||
		strings.Contains(lower, "sample")
}

func isCanonicalReleaseSuffix(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "7z", "zip", "rar", "par2", "nfo", "sfv", "srr", "mkv", "avi", "mp4", "mp3", "flac":
		return true
	default:
		return rarTokenRE.MatchString(token)
	}
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseCounterPairs(subject string) []counterPair {
	if strings.TrimSpace(subject) == "" {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]counterPair, 0, 4)
	for _, re := range []*regexp.Regexp{partMarkerRE, partLooseRE} {
		matches := re.FindAllStringSubmatch(subject, -1)
		for _, m := range matches {
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
			key := fmt.Sprintf("%d/%d", part, total)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, counterPair{Part: part, Total: total})
		}
	}

	return out
}

func parsePartInfo(subject string) (int, int) {
	if part, total := bestCounterAfterYEnc(subject); total > 0 {
		return part, total
	}

	return bestCounterPair(subject)
}

func parseFileInfo(subject string, articlePart, articleTotal int) (int, int) {
	if part, total := bestCounterBeforeYEnc(subject); total > 0 {
		return part, total
	}

	best := counterPair{}
	for _, pair := range parseCounterPairs(subject) {
		if pair.Part == articlePart && pair.Total == articleTotal {
			continue
		}
		if best.Total == 0 || pair.Total < best.Total || (pair.Total == best.Total && pair.Part < best.Part) {
			best = pair
		}
	}
	if best.Total <= 0 {
		return 0, 0
	}
	return best.Part, best.Total
}

func bestCounterPair(subject string) (int, int) {
	bestPart := 1
	bestTotal := 1

	for _, pair := range parseCounterPairs(subject) {
		if pair.Total > bestTotal || (pair.Total == bestTotal && pair.Part > bestPart) {
			bestPart = pair.Part
			bestTotal = pair.Total
		}
	}

	return bestPart, bestTotal
}

func bestCounterAfterYEnc(subject string) (int, int) {
	idx := strings.LastIndex(strings.ToLower(subject), "yenc")
	if idx < 0 || idx >= len(subject) {
		return 0, 0
	}
	return bestCounterPair(subject[idx:])
}

func bestCounterBeforeYEnc(subject string) (int, int) {
	idx := strings.LastIndex(strings.ToLower(subject), "yenc")
	if idx <= 0 {
		return 0, 0
	}
	return bestCounterPair(subject[:idx])
}

func parseStructuredData(subject string, raw map[string]any) structuredData {
	out := structuredData{}

	if m := yencNameRE.FindStringSubmatch(subject); len(m) > 0 {
		out.Name = firstNonEmpty(m[1], m[2])
	}
	out.Part = firstPositiveInt(extractRegexpInt(yencPartRE, subject), lookupInt(raw, "part"))
	out.Total = firstPositiveInt(extractRegexpInt(yencTotalRE, subject), lookupInt(raw, "total"))
	out.Size = firstPositiveInt64(extractRegexpInt64(yencSizeRE, subject), lookupInt64(raw, "size"), lookupInt64(raw, "bytes"))
	out.FileIndex = lookupInt(raw, "file_index")
	out.FileTotal = lookupInt(raw, "file_total")

	if out.Name == "" {
		out.Name = lookupString(raw, "name")
	}
	if out.Name == "" {
		out.Name = lookupString(raw, "filename")
	}

	out.Name = sanitizeFileName(out.Name)
	return out
}

func extractQuotedFilename(subject string) string {
	all := quotedFilenameRE.FindAllStringSubmatch(subject, -1)
	if len(all) == 0 {
		return ""
	}

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
	base = yencNameRE.ReplaceAllString(base, " ")
	base = yencPartRE.ReplaceAllString(base, " ")
	base = yencTotalRE.ReplaceAllString(base, " ")
	base = yencSizeRE.ReplaceAllString(base, " ")
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

func normalizePoster(poster string) string {
	poster = strings.ToLower(strings.TrimSpace(poster))
	poster = strings.ReplaceAll(poster, "<", " ")
	poster = strings.ReplaceAll(poster, ">", " ")
	poster = multiSpaceRE.ReplaceAllString(poster, " ")
	return normalizeKey(poster)
}

func extractMessageHost(messageID string) string {
	value := strings.TrimSpace(strings.Trim(messageID, "<>"))
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "@")
	if len(parts) < 2 {
		return ""
	}
	return normalizeKey(parts[len(parts)-1])
}

func parseXrefGroups(xref string) []string {
	fields := strings.Fields(strings.TrimSpace(xref))
	if len(fields) < 2 {
		return nil
	}

	out := make([]string, 0, len(fields)-1)
	for _, field := range fields[1:] {
		group := field
		if idx := strings.IndexByte(group, ':'); idx >= 0 {
			group = group[:idx]
		}
		group = normalizeKey(group)
		if group != "" {
			out = append(out, group)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func derivePostingWindow(postedAt *time.Time) string {
	if postedAt == nil || postedAt.IsZero() {
		return ""
	}
	utc := postedAt.UTC()
	return fmt.Sprintf("%s-%d", utc.Format("20060102"), utc.Hour()/6)
}

func deriveArticleBucket(articleNumber, window int64) int64 {
	if articleNumber <= 0 || window <= 0 {
		return 0
	}
	return (articleNumber / window) * window
}

func extractExtensionHint(value string) string {
	if value == "" {
		return ""
	}
	if ext := filepath.Ext(sanitizeFileName(value)); ext != "" {
		return strings.ToLower(ext)
	}
	if m := extensionHintRE.FindString(strings.ToLower(value)); m != "" {
		return strings.ToLower(m)
	}
	return ""
}

func lookupString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		for rawKey, rawValue := range raw {
			if !strings.EqualFold(strings.TrimSpace(rawKey), key) {
				continue
			}
			switch tv := rawValue.(type) {
			case string:
				if value := sanitizeFileName(tv); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func lookupInt(raw map[string]any, keys ...string) int {
	return int(lookupInt64(raw, keys...))
}

func lookupInt64(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		for rawKey, rawValue := range raw {
			if !strings.EqualFold(strings.TrimSpace(rawKey), key) {
				continue
			}
			switch tv := rawValue.(type) {
			case int:
				return int64(tv)
			case int32:
				return int64(tv)
			case int64:
				return tv
			case float64:
				return int64(tv)
			case string:
				parsed, err := strconv.ParseInt(strings.TrimSpace(tv), 10, 64)
				if err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func extractRegexpInt(re *regexp.Regexp, value string) int {
	return int(extractRegexpInt64(re, value))
}

func extractRegexpInt64(re *regexp.Regexp, value string) int64 {
	if re == nil {
		return 0
	}
	m := re.FindStringSubmatch(value)
	if len(m) < 2 {
		return 0
	}
	parsed, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func clampConfidence(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
