package releasecard

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const BodySchema = "gonzbnet.ReleaseCard/1.0"

type LocalRelease struct {
	LocalReleaseID    string
	GUID              string
	Title             string
	Category          string
	CategoryID        int
	Classification    string
	SizeBytes         int64
	PostedAt          *time.Time
	AddedAt           *time.Time
	FileCount         int
	CompletionPct     float64
	Groups            []string
	Files             []LocalFile
	HasPAR2           bool
	HasNFO            bool
	PasswordState     string
	Availability      float64
	TMDBID            int64
	TVDBID            int64
	IMDBID            string
	ExternalMedia     string
	ExternalTitle     string
	ExternalYear      int
	RuntimeSeconds    int
	PrimaryResolution string
	PrimaryVideoCodec string
	PrimaryAudioCodec string
	SourceKind        string
}

type LocalFile struct {
	ID           int64
	Name         string
	Subject      string
	Poster       string
	PostedAt     *time.Time
	SizeBytes    int64
	FileIndex    int
	IsPars       bool
	ArticleCount int
	TotalParts   int
	Segments     []LocalSegment
}

type LocalSegment struct {
	Number    int
	Bytes     int64
	MessageID string
}

type ReleaseCard struct {
	SchemaVersion      string     `json:"schema_version"`
	Type               string     `json:"type"`
	ReleaseID          string     `json:"release_id"`
	ManifestID         string     `json:"manifest_id,omitempty"`
	Title              string     `json:"title"`
	NormalizedTitle    string     `json:"normalized_title"`
	Category           []string   `json:"category"`
	NewznabCategories  []int      `json:"newznab_categories"`
	SizeBytes          int64      `json:"size_bytes"`
	PostedAt           string     `json:"posted_at,omitempty"`
	Groups             []string   `json:"groups"`
	FileCount          int        `json:"file_count"`
	SegmentCount       int        `json:"segment_count"`
	PosterHash         string     `json:"poster_hash,omitempty"`
	SubjectFingerprint string     `json:"subject_fingerprint"`
	FileFingerprint    string     `json:"file_fingerprint"`
	NZBGUID            *string    `json:"nzb_guid"`
	Media              Media      `json:"media"`
	Quality            Quality    `json:"quality"`
	Flags              Flags      `json:"flags"`
	Resolution         Resolution `json:"resolution"`
	Source             Source     `json:"source"`
	ExpiresAt          string     `json:"expires_at,omitempty"`
}

type Media struct {
	IMDBID  string `json:"imdb_id,omitempty"`
	TMDBID  int64  `json:"tmdb_id,omitempty"`
	TVDBID  int64  `json:"tvdb_id,omitempty"`
	Season  *int   `json:"season"`
	Episode *int   `json:"episode"`
	Year    int    `json:"year,omitempty"`
}

type Quality struct {
	Resolution string `json:"resolution,omitempty"`
	Source     string `json:"source,omitempty"`
	Codec      string `json:"codec,omitempty"`
	Audio      string `json:"audio,omitempty"`
}

type Flags struct {
	Passworded         string `json:"passworded"`
	EncryptedNames     bool   `json:"encrypted_names"`
	ContainsExecutable string `json:"contains_executable"`
	RequiresRepair     string `json:"requires_repair"`
	ObfuscatedSubjects bool   `json:"obfuscated_subjects"`
}

type Resolution struct {
	Status              string   `json:"status"`
	FetchPolicy         string   `json:"fetch_policy"`
	CompressedSizeBytes int64    `json:"compressed_size_bytes,omitempty"`
	ManifestSources     []string `json:"manifest_sources"`
}

type Source struct {
	Kind            string  `json:"kind"`
	Confidence      float64 `json:"confidence"`
	IndexerNameHash *string `json:"indexer_name_hash"`
}

type Projection struct {
	Card         ReleaseCard
	EventID      string
	SourceNodeID string
	PoolID       string
}

func MapLocalRelease(in LocalRelease) (ReleaseCard, error) {
	groups := normalizeStrings(in.Groups)
	files := normalizeFiles(in.Files)
	segmentCount := countSegments(files)

	fileFingerprint, err := fingerprint(fileFingerprintCore(files))
	if err != nil {
		return ReleaseCard{}, err
	}
	subjectFingerprint, err := fingerprint(subjectFingerprintCore(files))
	if err != nil {
		return ReleaseCard{}, err
	}
	posterHash, err := posterFingerprint(files)
	if err != nil {
		return ReleaseCard{}, err
	}

	normalizedTitle := NormalizeTitle(in.Title)
	releaseID, err := releaseID(releaseIdentityCore{
		NormalizedTitle:    normalizedTitle,
		SizeBytes:          in.SizeBytes,
		PostedAtDay:        postedAtDay(in.PostedAt),
		Groups:             groups,
		FileCount:          positiveInt(in.FileCount, len(files)),
		SegmentCount:       segmentCount,
		SubjectFingerprint: subjectFingerprint,
		FileFingerprint:    fileFingerprint,
	})
	if err != nil {
		return ReleaseCard{}, err
	}

	manifestID, err := manifestID(groups, files)
	if err != nil {
		return ReleaseCard{}, err
	}

	status := "metadata_only"
	if manifestID != "" {
		status = "local_manifest_available"
	}
	expiresAt := ""
	if in.PostedAt != nil {
		expiresAt = in.PostedAt.UTC().Add(90 * 24 * time.Hour).Format(time.RFC3339)
	}

	return ReleaseCard{
		SchemaVersion:      "1.0",
		Type:               "ReleaseCard",
		ReleaseID:          releaseID,
		ManifestID:         manifestID,
		Title:              strings.TrimSpace(in.Title),
		NormalizedTitle:    normalizedTitle,
		Category:           categories(in),
		NewznabCategories:  newznabCategories(in.CategoryID),
		SizeBytes:          in.SizeBytes,
		PostedAt:           formatOptionalTime(in.PostedAt),
		Groups:             groups,
		FileCount:          positiveInt(in.FileCount, len(files)),
		SegmentCount:       segmentCount,
		PosterHash:         posterHash,
		SubjectFingerprint: subjectFingerprint,
		FileFingerprint:    fileFingerprint,
		Media: Media{
			IMDBID:  in.IMDBID,
			TMDBID:  in.TMDBID,
			TVDBID:  in.TVDBID,
			Season:  nil,
			Episode: nil,
			Year:    in.ExternalYear,
		},
		Quality: Quality{
			Resolution: in.PrimaryResolution,
			Source:     in.ExternalMedia,
			Codec:      in.PrimaryVideoCodec,
			Audio:      in.PrimaryAudioCodec,
		},
		Flags: Flags{
			Passworded:         passwordState(in.PasswordState),
			EncryptedNames:     false,
			ContainsExecutable: "unknown",
			RequiresRepair:     repairState(in.HasPAR2),
			ObfuscatedSubjects: false,
		},
		Resolution: Resolution{
			Status:          status,
			FetchPolicy:     "local_only",
			ManifestSources: []string{},
		},
		Source: Source{
			Kind:            firstNonBlank(in.SourceKind, "local_indexer_cache"),
			Confidence:      clamp01(in.Availability),
			IndexerNameHash: nil,
		},
		ExpiresAt: expiresAt,
	}, nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func NormalizeTitle(title string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func HashBody(card ReleaseCard) (string, error) {
	hash, _, err := canonical.BodyHash(card)
	return hash, err
}

func Validate(card ReleaseCard, now time.Time, futureTolerance time.Duration) error {
	if strings.TrimSpace(card.SchemaVersion) != "1.0" || strings.TrimSpace(card.Type) != "ReleaseCard" {
		return fmt.Errorf("unsupported release card schema or type")
	}
	if !validHashID(card.ReleaseID, "rel") {
		return fmt.Errorf("release_id is invalid")
	}
	if strings.TrimSpace(card.Title) == "" || strings.TrimSpace(card.NormalizedTitle) == "" {
		return fmt.Errorf("title and normalized_title are required")
	}
	if NormalizeTitle(card.Title) != strings.TrimSpace(card.NormalizedTitle) {
		return fmt.Errorf("normalized_title does not match title")
	}
	if card.SizeBytes < 0 || card.FileCount < 0 || card.SegmentCount < 0 {
		return fmt.Errorf("release counts and size must not be negative")
	}
	if card.Source.Confidence < 0 || card.Source.Confidence > 1 {
		return fmt.Errorf("source confidence must be between 0 and 1")
	}
	for _, category := range card.NewznabCategories {
		if category <= 0 {
			return fmt.Errorf("newznab categories must be positive")
		}
	}
	for _, group := range card.Groups {
		if !validGroupName(group) {
			return fmt.Errorf("invalid group name")
		}
	}
	if strings.TrimSpace(card.ManifestID) != "" && !validHashID(card.ManifestID, "man") {
		return fmt.Errorf("manifest_id is invalid")
	}
	if strings.TrimSpace(card.SubjectFingerprint) == "" || strings.TrimSpace(card.FileFingerprint) == "" {
		return fmt.Errorf("subject_fingerprint and file_fingerprint are required")
	}
	futureTolerance = positiveDuration(futureTolerance, 2*time.Minute)
	postedAt, err := parseReleaseCardTime("posted_at", card.PostedAt)
	if err != nil {
		return err
	}
	if !now.IsZero() && postedAt != nil && postedAt.After(now.UTC().Add(futureTolerance)) {
		return fmt.Errorf("posted_at is too far in the future")
	}
	expiresAt, err := parseReleaseCardTime("expires_at", card.ExpiresAt)
	if err != nil {
		return err
	}
	if !now.IsZero() && expiresAt != nil && !expiresAt.After(now.UTC()) {
		return fmt.Errorf("release card expired")
	}
	expected, err := releaseID(releaseIdentityCore{
		NormalizedTitle:    strings.TrimSpace(card.NormalizedTitle),
		SizeBytes:          card.SizeBytes,
		PostedAtDay:        postedAtDay(postedAt),
		Groups:             normalizeStrings(card.Groups),
		FileCount:          card.FileCount,
		SegmentCount:       card.SegmentCount,
		SubjectFingerprint: strings.TrimSpace(card.SubjectFingerprint),
		FileFingerprint:    strings.TrimSpace(card.FileFingerprint),
	})
	if err != nil {
		return err
	}
	if expected != card.ReleaseID {
		return fmt.Errorf("release_id mismatch")
	}
	return nil
}

func parseReleaseCardTime(field, value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%s must be RFC3339", field)
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func validHashID(value, prefix string) bool {
	value = strings.TrimSpace(value)
	wantPrefix := prefix + "_"
	if !strings.HasPrefix(value, wantPrefix) || len(value) != len(wantPrefix)+52 {
		return false
	}
	for _, r := range value[len(wantPrefix):] {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return false
		}
	}
	return true
}

func validGroupName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || !strings.Contains(value, ".") {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' && r != '+' {
				return false
			}
		}
	}
	return true
}
