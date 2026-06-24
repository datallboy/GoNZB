package pgindex

import (
	"fmt"
	"strings"
)

type ReleaseDetailDiagnostics struct {
	PayloadComplete                 bool    `json:"payload_complete"`
	PayloadCompletenessKnown        bool    `json:"payload_completeness_known"`
	PayloadCompletionPct            float64 `json:"payload_completion_pct"`
	KnownBinaryCompletionPct        float64 `json:"known_binary_completion_pct"`
	ExpectedFileCountComplete       bool    `json:"expected_file_count_complete"`
	ExpectedFileCountKnown          bool    `json:"expected_file_count_known"`
	ExpectedArchiveFileCountKnown   bool    `json:"expected_archive_file_count_known"`
	MissingExpectedFileCount        int     `json:"missing_expected_file_count"`
	MissingExpectedArchiveFileCount int     `json:"missing_expected_archive_file_count"`
	HasPAR2Manifest                 bool    `json:"has_par2_manifest"`
	HasSFV                          bool    `json:"has_sfv"`
	ReadinessNote                   string  `json:"readiness_note"`
}

func buildReleaseDetailDiagnostics(release IndexerReleaseSummary, files []IndexerReleaseFileSummary) ReleaseDetailDiagnostics {
	diag := ReleaseDetailDiagnostics{
		PayloadComplete:               releasePayloadComplete(release),
		PayloadCompletenessKnown:      releasePayloadCompletenessKnown(release),
		PayloadCompletionPct:          releasePayloadCompletionPct(release),
		KnownBinaryCompletionPct:      release.CompletionPct,
		ExpectedFileCountComplete:     releaseExpectedFileCountComplete(release),
		ExpectedFileCountKnown:        release.ExpectedFileCount > 0,
		ExpectedArchiveFileCountKnown: release.ExpectedArchiveFileCount > 0,
	}
	if release.ExpectedFileCount > release.FileCount {
		diag.MissingExpectedFileCount = release.ExpectedFileCount - release.FileCount
	}
	nonPARFileCount := releaseNonPARFileCount(release)
	if release.ExpectedArchiveFileCount > nonPARFileCount {
		diag.MissingExpectedArchiveFileCount = release.ExpectedArchiveFileCount - nonPARFileCount
	}

	for _, file := range files {
		name := strings.ToLower(strings.TrimSpace(file.FileName))
		switch {
		case strings.HasSuffix(name, ".par2") && !strings.Contains(name, ".vol"):
			diag.HasPAR2Manifest = true
		case strings.HasSuffix(name, ".sfv"):
			diag.HasSFV = true
		}
	}

	diag.ReadinessNote = buildReleaseReadinessNote(release, diag)
	return diag
}

func releasePayloadComplete(release IndexerReleaseSummary) bool {
	if release.ArchiveCount > 0 && release.ExpectedArchiveFileCount <= 0 {
		return false
	}
	if release.ExpectedArchiveFileCount <= 0 {
		return true
	}
	return releaseNonPARFileCount(release) >= release.ExpectedArchiveFileCount
}

func releasePayloadCompletenessKnown(release IndexerReleaseSummary) bool {
	return release.ArchiveCount <= 0 || release.ExpectedArchiveFileCount > 0
}

func releasePayloadCompletionPct(release IndexerReleaseSummary) float64 {
	if release.ExpectedArchiveFileCount <= 0 {
		if release.ArchiveCount > 0 {
			return 0
		}
		return release.CompletionPct
	}
	pct := (float64(releaseNonPARFileCount(release)) / float64(release.ExpectedArchiveFileCount)) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

func releaseNonPARFileCount(release IndexerReleaseSummary) int {
	return max(release.FileCount-release.ParFileCount, 0)
}

func releaseExpectedFileCountComplete(release IndexerReleaseSummary) bool {
	return release.ExpectedFileCount <= 0 || release.FileCount >= release.ExpectedFileCount
}

func buildReleaseReadinessNote(release IndexerReleaseSummary, diag ReleaseDetailDiagnostics) string {
	if !diag.PayloadCompletenessKnown {
		if release.HasPAR2 && !diag.HasPAR2Manifest {
			return "Payload completeness is unknown. Archive inspection found archive payload files, but no base PAR2 target manifest has established the expected archive file count."
		}
		return "Payload completeness is unknown. Archive inspection found archive payload files, but no expected archive file count has been established yet."
	}
	if !diag.PayloadComplete {
		return "Payload files are still incomplete. This release should not generate a downloader-facing NZB yet."
	}
	if diag.ExpectedFileCountComplete {
		return ""
	}
	if diag.MissingExpectedFileCount <= 0 {
		return ""
	}
	if release.HasPAR2 && !diag.HasPAR2Manifest {
		return fmt.Sprintf("Payload files are complete, but %d expected auxiliary file(s) are still missing. The base PAR2 manifest is not present yet, so the gap is likely sidecar-only rather than payload loss.", diag.MissingExpectedFileCount)
	}
	return fmt.Sprintf("Payload files are complete, but %d expected auxiliary file(s) are still missing. The remaining gap is likely sidecar-only, such as PAR2, NFO, or SFV files.", diag.MissingExpectedFileCount)
}
