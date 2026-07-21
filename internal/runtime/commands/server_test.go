package commands

import "testing"

func TestHTTPListenAddress(t *testing.T) {
	tests := []struct {
		name        string
		bindAddress string
		port        string
		want        string
	}{
		{name: "default all interfaces", port: "8080", want: ":8080"},
		{name: "ipv4 loopback", bindAddress: "127.0.0.1", port: "18081", want: "127.0.0.1:18081"},
		{name: "ipv6 loopback", bindAddress: "::1", port: "18081", want: "[::1]:18081"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := httpListenAddress(test.bindAddress, test.port); got != test.want {
				t.Fatalf("httpListenAddress(%q, %q) = %q, want %q", test.bindAddress, test.port, got, test.want)
			}
		})
	}
}
