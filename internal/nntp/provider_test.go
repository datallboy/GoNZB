package nntp

import (
	"testing"
	"time"
)

func TestParseNNTPDateSupportsTwoDigitYearUTC(t *testing.T) {
	got := parseNNTPDate("Thu, 09 Apr 26 18:13:57 UTC")
	if got == nil {
		t.Fatal("expected parsed date, got nil")
	}

	want := time.Date(2026, time.April, 9, 18, 13, 57, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.Format(time.RFC3339))
	}
}

func TestParseOverviewLineParsesTwoDigitYearDate(t *testing.T) {
	line := "1881650125\tSubject Here\tposter@example\tThu, 09 Apr 26 18:13:57 UTC\t<message@example>\t\t740410\t0\tXref: news.easynews.com alt.binaries.sleazemovies:1881650125"

	got, ok := parseOverviewLine(line)
	if !ok {
		t.Fatal("expected overview line to parse")
	}
	if got.DateUTC == nil {
		t.Fatal("expected parsed overview date, got nil")
	}

	want := time.Date(2026, time.April, 9, 18, 13, 57, 0, time.UTC)
	if !got.DateUTC.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.DateUTC.Format(time.RFC3339))
	}
}
