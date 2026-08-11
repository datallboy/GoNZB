package transportpolicy

import (
	"strings"
	"testing"
)

func TestParseTraversalLocator(t *testing.T) {
	nodeID := "node_" + strings.Repeat("a", 52)
	got, err := ParseLocator("gonzb+ice://"+nodeID+"@connect.example/gonzbnet/v1", false, true)
	if err != nil {
		t.Fatalf("parse traversal locator: %v", err)
	}
	if got.Kind != LocatorICE || got.NodeID != nodeID || got.Coordinator != "connect.example" {
		t.Fatalf("unexpected locator: %+v", got)
	}
}

func TestTraversalLocatorDisabled(t *testing.T) {
	nodeID := "node_" + strings.Repeat("a", 52)
	err := ValidatePeerURL("gonzb+ice://"+nodeID+"@connect.example/gonzbnet/v1", false, false)
	if err == nil || !strings.Contains(err.Error(), "traversal.enabled is false") {
		t.Fatalf("expected disabled diagnostic, got %v", err)
	}
}

func TestTraversalLocatorRejectsCredentialsAndArbitraryShape(t *testing.T) {
	nodeID := "node_" + strings.Repeat("a", 52)
	for _, raw := range []string{
		"gonzb+ice://connect.example/gonzbnet/v1",
		"gonzb+ice://" + nodeID + ":secret@connect.example/gonzbnet/v1",
		"gonzb+ice://" + nodeID + "@connect.example/",
		"gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1?q=1",
		"gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1/../api",
		"gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1%5Cadmin",
	} {
		if _, err := ParseLocator(raw, false, true); err == nil {
			t.Fatalf("expected %q to fail", raw)
		}
	}
}

func TestJoinPreservesTraversalAuthority(t *testing.T) {
	nodeID := "node_" + strings.Repeat("a", 52)
	got, err := Join("gonzb+ice://"+nodeID+"@connect.example/gonzbnet/v1", "/outbox")
	if err != nil {
		t.Fatal(err)
	}
	want := "gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1/outbox"
	if got != want {
		t.Fatalf("join = %q, want %q", got, want)
	}
}

func TestTraversalRequestAllowsQueryWithoutWeakeningBaseLocator(t *testing.T) {
	nodeID := "node_" + strings.Repeat("a", 52)
	raw := "gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1/outbox?limit=100&since=evt_1"
	if err := ValidatePeerRequestURL(raw, false, true); err != nil {
		t.Fatalf("derived request URL rejected: %v", err)
	}
	if err := ValidatePeerURL(raw, false, true); err == nil {
		t.Fatal("query-bearing URL was accepted as a base locator")
	}
}

func TestTraversalRequestRejectsFragmentAndUnsafePath(t *testing.T) {
	nodeID := "node_" + strings.Repeat("a", 52)
	for _, raw := range []string{
		"gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1/outbox#fragment",
		"gonzb+ice://" + nodeID + "@connect.example/gonzbnet/v1/../api?limit=1",
	} {
		if err := ValidatePeerRequestURL(raw, false, true); err == nil {
			t.Fatalf("unsafe request URL accepted: %q", raw)
		}
	}
}

func FuzzParseTraversalLocator(f *testing.F) {
	f.Add("gonzb+ice://node_" + strings.Repeat("a", 52) + "@connect.example/gonzbnet/v1")
	f.Add("gonzb+ice://node_bad@connect.example/../api")
	f.Fuzz(func(t *testing.T, raw string) {
		locator, err := ParseLocator(raw, false, true)
		if err == nil && locator.Kind == LocatorICE {
			if !nodeIDPattern.MatchString(locator.NodeID) || !safeFederationPath(locator.URL.Path) {
				t.Fatalf("invalid traversal locator accepted: %+v", locator)
			}
		}
	})
}
