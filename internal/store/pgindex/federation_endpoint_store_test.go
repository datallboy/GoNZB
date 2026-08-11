package pgindex

import "testing"

func TestEndpointTransport(t *testing.T) {
	for raw, want := range map[string]string{
		"https://node.example/gonzbnet/v1":               "https",
		"http://localhost:8080/gonzbnet/v1":              "https",
		"gonzb+ice://node_a@connect.example/gonzbnet/v1": "ice",
	} {
		got, err := endpointTransport(raw)
		if err != nil || got != want {
			t.Fatalf("endpointTransport(%q) = %q, %v", raw, got, err)
		}
	}
	if _, err := endpointTransport("file:///etc/passwd"); err == nil {
		t.Fatal("unsafe endpoint accepted")
	}
}
