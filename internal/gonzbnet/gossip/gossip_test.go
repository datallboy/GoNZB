package gossip

import "testing"

func TestNormalizeAndForwardTTL(t *testing.T) {
	if got := NormalizeTTL(10, 4); got != 4 {
		t.Fatalf("expected ttl clamp to 4, got %d", got)
	}
	if got := NormalizeTTL(-1, 4); got != 0 {
		t.Fatalf("expected negative ttl to become 0, got %d", got)
	}
	if got := ForwardTTL(4); got != 3 {
		t.Fatalf("expected forward ttl 3, got %d", got)
	}
	if got := ForwardTTL(1); got != 0 {
		t.Fatalf("expected ttl 1 to stop forwarding, got %d", got)
	}
}

func TestFilterPeersCanBeDisabledCompletely(t *testing.T) {
	peers := []string{" https://a.example/gonzbnet/v1/ ", "https://a.example/gonzbnet/v1", "https://b.example/gonzbnet/v1"}
	if got := FilterPeers(peers, false, 10); len(got) != 0 {
		t.Fatalf("expected disabled peer exchange to return no peers, got %#v", got)
	}
	got := FilterPeers(peers, true, 1)
	if len(got) != 1 || got[0] != "https://a.example/gonzbnet/v1" {
		t.Fatalf("unexpected filtered peers: %#v", got)
	}
}
