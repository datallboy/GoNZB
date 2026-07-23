package controllers

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/categories/newsnab"
	"github.com/datallboy/gonzb/internal/domain"
)

func TestBuildCapCategoriesUsesCanonicalNewsnabTree(t *testing.T) {
	cats := buildCapCategories()
	if len(cats) == 0 {
		t.Fatalf("expected cap categories")
	}
	if cats[0].ID != newsnab.ConsoleRoot {
		t.Fatalf("expected console root first, got %+v", cats[0])
	}
	foundMoviesHD := false
	for _, root := range cats {
		for _, sub := range root.SubCats {
			if sub.ID == newsnab.MoviesHD {
				foundMoviesHD = true
				break
			}
		}
	}
	if !foundMoviesHD {
		t.Fatalf("expected MoviesHD in cap categories, got %+v", cats)
	}
}

func TestBuildRSSResponseUsesNumericCategoryAttr(t *testing.T) {
	resp := buildRSSResponse([]*domain.Release{{
		ID:          "rel-1",
		Title:       "Example",
		Size:        1024,
		PublishDate: time.Unix(1, 0).UTC(),
		Category:    "2040",
	}}, "http://localhost:8080", "", 0, 1)

	if len(resp.Channel.Items) != 1 {
		t.Fatalf("expected one rss item, got %+v", resp.Channel.Items)
	}
	item := resp.Channel.Items[0]
	if item.Category != newsnab.DisplayName(newsnab.MoviesHD) {
		t.Fatalf("expected display category %q, got %+v", newsnab.DisplayName(newsnab.MoviesHD), item)
	}
	if len(item.Attributes) == 0 || item.Attributes[0].Value != "2040" {
		t.Fatalf("expected numeric category attr, got %+v", item.Attributes)
	}
}

func TestBuildRSSResponseUsesLocalDownloadLinks(t *testing.T) {
	resp := buildRSSResponse([]*domain.Release{{
		ID:          "local-composite-id",
		Title:       "Federated Example",
		Source:      "gonzbnet",
		GUID:        "rel_federated",
		Size:        2048,
		PublishDate: time.Unix(1, 0).UTC(),
		Category:    "2040",
	}}, "http://local.example", "token", 0, 1)

	if len(resp.Channel.Items) != 1 {
		t.Fatalf("expected one rss item")
	}
	item := resp.Channel.Items[0]
	expected := "http://local.example/api?t=get&id=local-composite-id&apikey=token"
	if item.Link != expected || item.Enclosure.URL != expected {
		t.Fatalf("expected local download URL %q, got link=%q enclosure=%q", expected, item.Link, item.Enclosure.URL)
	}
}

func TestNewznabCategoryParsingAndRootFiltering(t *testing.T) {
	categories := parseNewznabCategories("2000, 2040,invalid,2040")
	if len(categories) != 2 || categories[0] != 2000 || categories[1] != 2040 {
		t.Fatalf("unexpected parsed categories: %v", categories)
	}

	results := filterNewznabCategories([]*domain.Release{
		{ID: "movie", Category: "2040"},
		{ID: "tv", Category: "5040"},
		{ID: "unknown", Category: "not-a-category"},
	}, []int{newsnab.MoviesRoot})
	if len(results) != 1 || results[0].ID != "movie" {
		t.Fatalf("expected only movie-root result, got %+v", results)
	}
}

func TestBuildRSSResponseReportsRequestedWindow(t *testing.T) {
	resp := buildRSSResponse([]*domain.Release{{
		ID:          "rel-2",
		Title:       "Page two",
		PublishDate: time.Unix(2, 0).UTC(),
		Category:    "2040",
	}}, "http://localhost:8080", "", 100, 235)

	if resp.Channel.Response.Offset != 100 || resp.Channel.Response.Total != 235 {
		t.Fatalf("unexpected response window: %+v", resp.Channel.Response)
	}
}
