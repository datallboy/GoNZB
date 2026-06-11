package pgindex

import (
	"fmt"
	"strings"
)

type ReleaseDetailDiagnostics struct {
	PayloadComplete           bool   `json:"payload_complete"`
	ExpectedFileCountComplete bool   `json:"expected_file_count_complete"`
	MissingExpectedFileCount  int    `json:"missing_expected_file_count"`
	HasPAR2Manifest           bool   `json:"has_par2_manifest"`
	HasSFV                    bool   `json:"has_sfv"`
	ReadinessNote             string `json:"readiness_note"`
}

func buildReleaseDetailDiagnostics(release IndexerReleaseSummary, files []IndexerReleaseFileSummary) ReleaseDetailDiagnostics {
	diag := ReleaseDetailDiagnostics{
		PayloadComplete:           releasePayloadComplete(release),
		ExpectedFileCountComplete: releaseExpectedFileCountComplete(release),
	}
	if release.ExpectedFileCount > release.FileCount {
		diag.MissingExpectedFileCount = release.ExpectedFileCount - release.FileCount
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
	if release.ExpectedArchiveFileCount <= 0 {
		return true
	}
	return max(release.FileCount-release.ParFileCount, 0) >= release.ExpectedArchiveFileCount
}

func releaseExpectedFileCountComplete(release IndexerReleaseSummary) bool {
	return release.ExpectedFileCount <= 0 || release.FileCount >= release.ExpectedFileCount
}

func buildReleaseReadinessNote(release IndexerReleaseSummary, diag ReleaseDetailDiagnostics) string {
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
