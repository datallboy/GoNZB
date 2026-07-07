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
	payloadComplete, payloadKnown, payloadPct := releasePayloadState(release, files)
	diag := ReleaseDetailDiagnostics{
		PayloadComplete:               payloadComplete,
		PayloadCompletenessKnown:      payloadKnown,
		PayloadCompletionPct:          payloadPct,
		KnownBinaryCompletionPct:      release.CompletionPct,
		ExpectedFileCountComplete:     releaseExpectedFileCountComplete(release),
		ExpectedFileCountKnown:        release.ExpectedFileCount > 0,
		ExpectedArchiveFileCountKnown: release.ExpectedArchiveFileCount > 0,
	}
	if release.ExpectedFileCount > release.FileCount {
		diag.MissingExpectedFileCount = release.ExpectedFileCount - release.FileCount
	}
	payloadFileCount := releasePayloadFileCount(release, files)
	if release.ExpectedArchiveFileCount > payloadFileCount {
		diag.MissingExpectedArchiveFileCount = release.ExpectedArchiveFileCount - payloadFileCount
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

func releasePayloadState(release IndexerReleaseSummary, files []IndexerReleaseFileSummary) (complete bool, known bool, pct float64) {
	if release.ArchiveCount > 0 && release.ExpectedArchiveFileCount <= 0 {
		return false, false, 0
	}
	if release.ExpectedArchiveFileCount > 0 {
		payloadFiles := releasePayloadFileCount(release, files)
		pct := (float64(payloadFiles) / float64(release.ExpectedArchiveFileCount)) * 100
		if pct > 100 {
			pct = 100
		}
		return payloadFiles >= release.ExpectedArchiveFileCount, true, pct
	}

	payloadFiles := releasePayloadFiles(files)
	if len(payloadFiles) == 0 {
		return release.CompletionPct >= 100, true, release.CompletionPct
	}

	observedParts := 0
	expectedParts := 0
	for _, file := range payloadFiles {
		if file.ObservedParts > 0 && file.TotalParts <= 0 {
			return false, false, 0
		}
		observedParts += file.ObservedParts
		expectedParts += max(file.TotalParts, file.ObservedParts)
	}
	if expectedParts <= 0 {
		return false, false, 0
	}
	pct = (float64(observedParts) / float64(expectedParts)) * 100
	if pct > 100 {
		pct = 100
	}
	return observedParts >= expectedParts, true, pct
}

func releasePayloadFileCount(release IndexerReleaseSummary, files []IndexerReleaseFileSummary) int {
	payloadFiles := releasePayloadFiles(files)
	if len(payloadFiles) == 0 {
		return max(release.FileCount-release.ParFileCount, 0)
	}
	return len(payloadFiles)
}

func releasePayloadFiles(files []IndexerReleaseFileSummary) []IndexerReleaseFileSummary {
	out := make([]IndexerReleaseFileSummary, 0, len(files))
	for _, file := range files {
		if releaseFileIsAuxiliary(file.FileName, file.IsPars) {
			continue
		}
		out = append(out, file)
	}
	return out
}

func releaseFileIsAuxiliary(fileName string, isPAR bool) bool {
	if isPAR {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(lower, ".nfo") ||
		strings.HasSuffix(lower, ".sfv") ||
		strings.HasSuffix(lower, ".srr") ||
		strings.HasSuffix(lower, ".srs") ||
		strings.HasSuffix(lower, ".nzb")
}

func releaseExpectedFileCountComplete(release IndexerReleaseSummary) bool {
	return release.ExpectedFileCount <= 0 || release.FileCount >= release.ExpectedFileCount
}

func buildReleaseReadinessNote(release IndexerReleaseSummary, diag ReleaseDetailDiagnostics) string {
	if !diag.PayloadCompletenessKnown {
		if release.ArchiveCount <= 0 {
			return "Payload completeness is unknown. The main payload has article evidence, but no authoritative total part count has been established yet."
		}
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
