package transportpolicy

import "testing"

func TestValidateHTTPURLRequiresHTTPSByDefault(t *testing.T) {
	if err := ValidateHTTPURL("https://peer.example/gonzbnet/v1", false); err != nil {
		t.Fatalf("expected https peer to pass: %v", err)
	}
	if err := ValidateHTTPURL("http://peer.example/gonzbnet/v1", false); err == nil {
		t.Fatalf("expected non-local http peer to fail")
	}
}

func TestValidateHTTPURLAllowsExplicitLocalDevelopment(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080/gonzbnet/v1",
		"http://127.0.0.1:8080/gonzbnet/v1",
		"http://[::1]:8080/gonzbnet/v1",
	} {
		if err := ValidateHTTPURL(raw, true); err != nil {
			t.Fatalf("expected local development url %q to pass: %v", raw, err)
		}
	}
	if err := ValidateHTTPURL("http://peer.example/gonzbnet/v1", true); err == nil {
		t.Fatalf("expected non-local http peer to fail even when local development is enabled")
	}
}

func TestValidateWebSocketURLRequiresWSSByDefault(t *testing.T) {
	if err := ValidateWebSocketURL("wss://peer.example/gonzbnet/v1/ws", false); err != nil {
		t.Fatalf("expected wss peer to pass: %v", err)
	}
	if err := ValidateWebSocketURL("ws://peer.example/gonzbnet/v1/ws", false); err == nil {
		t.Fatalf("expected non-local ws peer to fail")
	}
}
