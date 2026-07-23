package transportpolicy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ValidateHTTPURL(rawURL string, allowInsecureLocal bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if allowInsecureLocal && isLocalDevelopmentHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("insecure peer http is allowed only for explicit local development")
	default:
		return fmt.Errorf("peer url must use https")
	}
}

func ValidateWebSocketURL(rawURL string, allowInsecureLocal bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "wss":
		return nil
	case "ws":
		if allowInsecureLocal && isLocalDevelopmentHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("insecure peer websocket is allowed only for explicit local development")
	default:
		return fmt.Errorf("peer websocket url must use wss")
	}
}

func isLocalDevelopmentHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
