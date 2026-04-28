package inspect

import "testing"

func TestArchiveMediaPrefixLimitUsesLowerDefaultBudget(t *testing.T) {
	limit := archiveMediaPrefixLimit("movie.7z/feature.film.2026.mkv", 512*1024*1024, 0)
	if want := int64(64 * 1024 * 1024); limit != want {
		t.Fatalf("expected default prefix limit %d, got %d", want, limit)
	}
}

func TestArchiveMediaLimitsUseSmallerSampleBudget(t *testing.T) {
	prefix := archiveMediaPrefixLimit("Release/Sample/sample-video.mkv", 512*1024*1024, 0)
	if want := int64(24 * 1024 * 1024); prefix != want {
		t.Fatalf("expected sample prefix limit %d, got %d", want, prefix)
	}

	output := archiveMediaOutputLimit("Release/Sample/sample-video.mkv", 0)
	if want := int64(16 * 1024 * 1024); output != want {
		t.Fatalf("expected sample output limit %d, got %d", want, output)
	}
}

func TestArchiveMediaOutputLimitHonorsMaxBytesButKeepsMinimum(t *testing.T) {
	limit := archiveMediaOutputLimit("feature.film.2026.mkv", 4*1024*1024)
	if want := minArchiveMediaOutputBytes; limit != want {
		t.Fatalf("expected minimum output limit %d, got %d", want, limit)
	}
}
