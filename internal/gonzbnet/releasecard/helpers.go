package releasecard

import (
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
)

type releaseIdentityCore struct {
	NormalizedTitle    string   `json:"normalized_title"`
	SizeBytes          int64    `json:"size_bytes"`
	PostedAtDay        string   `json:"posted_at_day"`
	Groups             []string `json:"groups"`
	FileCount          int      `json:"file_count"`
	SegmentCount       int      `json:"segment_count"`
	SubjectFingerprint string   `json:"subject_fingerprint"`
	FileFingerprint    string   `json:"file_fingerprint"`
}

type fileFingerprintItem struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	FileIndex    int    `json:"file_index"`
	IsPars       bool   `json:"is_pars"`
	ArticleCount int    `json:"article_count"`
	TotalParts   int    `json:"total_parts"`
}

type subjectFingerprintItem struct {
	FileIndex int    `json:"file_index"`
	Name      string `json:"name"`
	Subject   string `json:"subject"`
}

func releaseID(core releaseIdentityCore) (string, error) {
	payload, err := canonical.Marshal(core)
	if err != nil {
		return "", err
	}
	return canonical.HashID("rel", payload), nil
}

func ManifestCoreForLocalRelease(in LocalRelease) (manifest.ManifestCore, error) {
	groups := normalizeStrings(in.Groups)
	files := normalizeFiles(in.Files)
	if len(groups) == 0 || len(files) == 0 {
		return manifest.ManifestCore{}, nil
	}
	coreFiles := make([]manifest.ManifestFile, 0, len(files))
	poster := ""
	baseFiles := 0
	volumeFiles := 0
	for _, file := range files {
		if len(file.Segments) == 0 {
			return manifest.ManifestCore{}, nil
		}
		segments := make([]manifest.ManifestSegment, 0, len(file.Segments))
		for _, segment := range file.Segments {
			if strings.TrimSpace(segment.MessageID) == "" || segment.Number <= 0 || segment.Bytes <= 0 {
				return manifest.ManifestCore{}, nil
			}
			segments = append(segments, manifest.ManifestSegment{
				Number:    segment.Number,
				Bytes:     segment.Bytes,
				MessageID: strings.TrimSpace(segment.MessageID),
			})
		}
		sort.Slice(segments, func(i, j int) bool {
			return segments[i].Number < segments[j].Number
		})
		if poster == "" {
			poster = strings.TrimSpace(file.Poster)
		}
		if file.IsPars {
			volumeFiles++
		} else {
			baseFiles++
		}
		coreFiles = append(coreFiles, manifest.ManifestFile{
			Name:      strings.TrimSpace(file.Name),
			Subject:   strings.TrimSpace(file.Subject),
			Date:      formatOptionalTime(file.PostedAt),
			SizeBytes: file.SizeBytes,
			Segments:  segments,
		})
	}
	sort.Slice(coreFiles, func(i, j int) bool {
		if coreFiles[i].Name == coreFiles[j].Name {
			return coreFiles[i].SizeBytes < coreFiles[j].SizeBytes
		}
		return coreFiles[i].Name < coreFiles[j].Name
	})
	return manifest.ManifestCore{
		Groups:   groups,
		Poster:   poster,
		PostedAt: formatOptionalTime(in.PostedAt),
		Files:    coreFiles,
		PAR2:     manifest.PAR2{Present: in.HasPAR2, BaseFiles: baseFiles, VolumeFiles: volumeFiles},
		NZB:      manifest.NZBInfo{Generator: "GoNZBNet", XMLCharset: "utf-8"},
	}, nil
}

func manifestID(in LocalRelease) (string, error) {
	core, err := ManifestCoreForLocalRelease(in)
	if err != nil || len(core.Files) == 0 {
		return "", err
	}
	id, _, err := manifest.ComputeID(core)
	return id, err
}

func normalizeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeFiles(in []LocalFile) []LocalFile {
	out := append([]LocalFile(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].FileIndex == out[j].FileIndex {
			return strings.TrimSpace(out[i].Name) < strings.TrimSpace(out[j].Name)
		}
		return out[i].FileIndex < out[j].FileIndex
	})
	for i := range out {
		out[i].Name = strings.TrimSpace(out[i].Name)
		out[i].Subject = strings.TrimSpace(out[i].Subject)
		out[i].Poster = strings.TrimSpace(out[i].Poster)
		sort.Slice(out[i].Segments, func(a, b int) bool {
			return out[i].Segments[a].Number < out[i].Segments[b].Number
		})
	}
	return out
}

func countSegments(files []LocalFile) int {
	total := 0
	for _, file := range files {
		switch {
		case len(file.Segments) > 0:
			total += len(file.Segments)
		case file.ArticleCount > 0:
			total += file.ArticleCount
		case file.TotalParts > 0:
			total += file.TotalParts
		}
	}
	return total
}

func fileFingerprintCore(files []LocalFile) []fileFingerprintItem {
	out := make([]fileFingerprintItem, 0, len(files))
	for _, file := range files {
		out = append(out, fileFingerprintItem{
			Name:         file.Name,
			SizeBytes:    file.SizeBytes,
			FileIndex:    file.FileIndex,
			IsPars:       file.IsPars,
			ArticleCount: positiveInt(file.ArticleCount, len(file.Segments)),
			TotalParts:   file.TotalParts,
		})
	}
	return out
}

func subjectFingerprintCore(files []LocalFile) []subjectFingerprintItem {
	out := make([]subjectFingerprintItem, 0, len(files))
	for _, file := range files {
		subject := strings.TrimSpace(file.Subject)
		if subject == "" {
			subject = file.Name
		}
		out = append(out, subjectFingerprintItem{
			FileIndex: file.FileIndex,
			Name:      file.Name,
			Subject:   subject,
		})
	}
	return out
}

func fingerprint(v any) (string, error) {
	hash, _, err := canonical.BodyHash(v)
	return hash, err
}

func posterFingerprint(files []LocalFile) (string, error) {
	posters := make([]string, 0, len(files))
	for _, file := range files {
		if poster := strings.TrimSpace(strings.ToLower(file.Poster)); poster != "" {
			posters = append(posters, poster)
		}
	}
	posters = normalizeStrings(posters)
	if len(posters) == 0 {
		return "", nil
	}
	return fingerprint(posters)
}

func postedAtDay(in *time.Time) string {
	if in == nil || in.IsZero() {
		return ""
	}
	return in.UTC().Format("2006-01-02")
}

func formatOptionalTime(in *time.Time) string {
	if in == nil || in.IsZero() {
		return ""
	}
	return in.UTC().Format(time.RFC3339)
}

func categories(in LocalRelease) []string {
	values := []string{in.Classification, in.Category}
	return normalizeStrings(values)
}

func newznabCategories(categoryID int) []int {
	if categoryID <= 0 {
		return []int{}
	}
	return []int{categoryID}
}

func positiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func passwordState(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "not_passworded":
		return "no"
	case "password_known":
		return "yes"
	case "password_unknown", "":
		return "unknown"
	default:
		return "unknown"
	}
}

func repairState(hasPAR2 bool) string {
	if hasPAR2 {
		return "repairable"
	}
	return "unknown"
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
