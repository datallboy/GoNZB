package newsnab

import "testing"

func TestResolveReleaseCategoryMovieHD(t *testing.T) {
	got := ResolveReleaseCategory(ReleaseAttributes{
		Classification:    "video",
		PrimaryResolution: "1080p",
		Title:             "Example.Movie.2026.1080p.BluRay.x265-GRP",
	})
	if got.ID != MoviesHD {
		t.Fatalf("expected MoviesHD, got %+v", got)
	}
	if got.RootSlug != "movies" {
		t.Fatalf("expected movies root slug, got %+v", got)
	}
}

func TestResolveReleaseCategoryTVByEpisodePattern(t *testing.T) {
	got := ResolveReleaseCategory(ReleaseAttributes{
		Classification:    "video",
		PrimaryResolution: "1080p",
		Title:             "Example.Show.S01E01.1080p.WEB-DL.x265-GRP",
	})
	if got.ID != TVHD {
		t.Fatalf("expected TVHD, got %+v", got)
	}
}

func TestResolveReleaseCategoryMovieWebDLDoesNotBecomeTV(t *testing.T) {
	got := ResolveReleaseCategory(ReleaseAttributes{
		PrimaryResolution: "1080p",
		Title:             "Example.Movie.2026.1080p.WEB-DL.x265-GRP",
	})
	if got.ID != MoviesHD {
		t.Fatalf("expected MoviesHD, got %+v", got)
	}
}

func TestResolveReleaseCategoryLeavesGenericVideoAsMisc(t *testing.T) {
	got := ResolveReleaseCategory(ReleaseAttributes{
		Classification: "video",
		Title:          "Opaque.Payload.Release",
	})
	if got.ID != OtherMisc {
		t.Fatalf("expected OtherMisc, got %+v", got)
	}
}

func TestResolveReleaseCategoryConsoleSwitch(t *testing.T) {
	got := ResolveReleaseCategory(ReleaseAttributes{
		Title: "Example.Game.NSW.Switch-GRP",
	})
	if got.ID != ConsoleSwitch {
		t.Fatalf("expected ConsoleSwitch, got %+v", got)
	}
}

func TestParseNameSupportsDisplayNames(t *testing.T) {
	id, ok := ParseName("Movies > HD")
	if !ok || id != MoviesHD {
		t.Fatalf("expected MoviesHD, got id=%d ok=%t", id, ok)
	}
}

func TestIDsForBrowseReturnsRootAndSubcategories(t *testing.T) {
	ids := IDsForBrowse("tv", "all")
	if len(ids) == 0 {
		t.Fatalf("expected tv browse ids")
	}
	seenHD := false
	for _, id := range ids {
		if id == TVHD {
			seenHD = true
			break
		}
	}
	if !seenHD {
		t.Fatalf("expected tv browse ids to include TVHD, got %+v", ids)
	}
}
