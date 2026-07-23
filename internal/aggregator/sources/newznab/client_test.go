package newznab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

	client := New("test", server.URL, "/api", "token", false, OutboundPolicy{AllowPrivateAddresses: true})
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

func TestSearchBlocksPrivateAddressByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("blocked private source should not receive a request")
	}))
	defer server.Close()

	client := New("test", server.URL, "/api", "token", false)
	_, err := client.Search(context.Background(), aggregator.SearchRequest{Type: aggregator.SearchTypeGeneric})
	if err == nil || !strings.Contains(err.Error(), "blocked by the Newznab source policy") {
		t.Fatalf("expected private-address policy error, got %v", err)
	}
}

func TestSearchAllowsExplicitPrivateCIDR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel></channel></rss>`))
	}))
	defer server.Close()

	client := New("test", server.URL, "/api", "token", false, OutboundPolicy{
		AllowedCIDRs: []string{"127.0.0.1/32"},
	})
	if _, err := client.Search(context.Background(), aggregator.SearchRequest{Type: aggregator.SearchTypeGeneric}); err != nil {
		t.Fatalf("search through explicit CIDR allowlist: %v", err)
	}
}

func TestConnectionRequestsCapabilities(t *testing.T) {
	var gotType, gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.URL.Query().Get("t")
		gotKey = r.URL.Query().Get("apikey")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<caps></caps>`))
	}))
	defer server.Close()

	client := New("test", server.URL, "/api", "token", false, OutboundPolicy{AllowPrivateAddresses: true})
	if err := client.TestConnection(t.Context()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if gotType != "caps" || gotKey != "token" {
		t.Fatalf("unexpected capabilities request: t=%q apikey=%q", gotType, gotKey)
	}
}
