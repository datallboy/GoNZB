package naming

import (
	"strings"
	"testing"
)

func TestNZBFilename(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Example Release 2026 1080p", want: "Example Release 2026 1080p.nzb"},
		{name: "Example.Feature.2026.2160p.WEB-DL.DDP5.1.DV.HDR.H.265-GROUP.mkv", want: "Example.Feature.2026.2160p.WEB-DL.DDP5.1.DV.HDR.H.265-GROUP.nzb"},
		{name: "Show/S01E01: Pilot?.nzb", want: "Show_S01E01_ Pilot_.nzb"},
		{name: "Movie.2026.2160p.H.265-GROUP", want: "Movie.2026.2160p.H.265-GROUP.nzb"},
		{name: "../\r\n", want: "release.nzb"},
		{name: " Already.NZB ", want: "Already.nzb"},
	}
	for _, test := range tests {
		if got := NZBFilename(test.name); got != test.want {
			t.Errorf("NZBFilename(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestNZBFilenameLimitsLongNames(t *testing.T) {
	got := NZBFilename(strings.Repeat("a", maxNZBNameRunes+20))
	if len([]rune(strings.TrimSuffix(got, ".nzb"))) > maxNZBNameRunes {
		t.Fatalf("filename was not limited: %d runes", len([]rune(got)))
	}
}
