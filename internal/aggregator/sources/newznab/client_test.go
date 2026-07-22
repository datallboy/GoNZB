package newznab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datallboy/gonzb/internal/aggregator"
)

func TestSearchForwardsLimitAndCategories(t *testing.T) {
	var gotLimit, gotCategories string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		gotCategories = r.URL.Query().Get("cat")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel></channel></rss>`))
	}))
	defer server.Close()

	client := New("test", server.URL, "/api", "token", false)
	_, err := client.Search(context.Background(), aggregator.SearchRequest{
		Type:       aggregator.SearchTypeGeneric,
		Query:      "example",
		Limit:      250,
		Categories: []int{2000, 2040},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if gotLimit != "250" || gotCategories != "2000,2040" {
		t.Fatalf("unexpected forwarded search window: limit=%q cat=%q", gotLimit, gotCategories)
	}
}
