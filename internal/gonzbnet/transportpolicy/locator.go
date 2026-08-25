package transportpolicy

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const TraversalScheme = "gonzb+ice"

var nodeIDPattern = regexp.MustCompile(`^node_[a-z2-7]{52}$`)

type LocatorKind string

const (
	LocatorHTTPS LocatorKind = "https"
	LocatorICE   LocatorKind = "ice"
)

type Locator struct {
	Kind        LocatorKind
	URL         *url.URL
	NodeID      string
	Coordinator string
}

// ParseLocator validates a federation transport locator. Traversal locators
// are accepted only when the local experimental module is explicitly enabled.
func ParseLocator(raw string, allowInsecureLocal, traversalEnabled bool) (Locator, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Locator{}, fmt.Errorf("peer locator must be an absolute URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		if err := ValidateHTTPURL(raw, allowInsecureLocal); err != nil {
			return Locator{}, err
		}
		return Locator{Kind: LocatorHTTPS, URL: parsed}, nil
	case TraversalScheme:
		if !traversalEnabled {
			return Locator{}, fmt.Errorf("gonzb+ice traversal locator is unsupported while gonzbnet.traversal.enabled is false")
		}
		if parsed.User == nil || parsed.User.Username() == "" {
			return Locator{}, fmt.Errorf("gonzb+ice locator requires a node ID")
		}
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return Locator{}, fmt.Errorf("gonzb+ice locator must not contain a password")
		}
		nodeID := strings.ToLower(parsed.User.Username())
		if !nodeIDPattern.MatchString(nodeID) {
			return Locator{}, fmt.Errorf("gonzb+ice locator contains an invalid node ID")
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return Locator{}, fmt.Errorf("gonzb+ice locator must not contain a query or fragment")
		}
		if parsed.Path == "" || parsed.Path == "/" {
			return Locator{}, fmt.Errorf("gonzb+ice locator requires a federation base path")
		}
		if !safeFederationPath(parsed.Path) {
			return Locator{}, fmt.Errorf("gonzb+ice locator contains an unsafe federation path")
		}
		return Locator{Kind: LocatorICE, URL: parsed, NodeID: nodeID, Coordinator: strings.ToLower(parsed.Host)}, nil
	default:
		return Locator{}, fmt.Errorf("peer locator must use https or gonzb+ice")
	}
}

func safeFederationPath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func ValidatePeerURL(raw string, allowInsecureLocal, traversalEnabled bool) error {
	_, err := ParseLocator(raw, allowInsecureLocal, traversalEnabled)
	return err
}

// ValidatePeerRequestURL validates a concrete request URL derived from an
// already validated peer locator. Request queries are allowed because signed
// federation operations use cursors and invitations, but fragments remain
// forbidden and the scheme, authority, node ID, and path receive the same
// validation as a base locator.
func ValidatePeerRequestURL(raw string, allowInsecureLocal, traversalEnabled bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("peer request must be an absolute URL")
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("peer request must not contain a fragment")
	}
	base := *parsed
	base.RawQuery = ""
	base.ForceQuery = false
	_, err = ParseLocator(base.String(), allowInsecureLocal, traversalEnabled)
	return err
}

// Join appends a protocol-relative path without changing the locator scheme,
// node identity, or coordinator authority.
func Join(base, suffix string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("peer locator must be an absolute URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}

func WellKnownURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("peer locator must be an absolute URL")
	}
	parsed.Path = "/.well-known/gonzbnet"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
