package peertransport

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// Transport centralizes outbound federation routing while retaining the
// standard HTTPS transport for mixed-version and traversal-disabled peers.
type Transport struct {
	Direct    http.RoundTripper
	Traversal http.RoundTripper
}

func (t Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, fmt.Errorf("federation request URL is required")
	}
	if strings.EqualFold(request.URL.Scheme, "gonzb+ice") {
		if t.Traversal == nil {
			return nil, fmt.Errorf("gonzb+ice traversal transport is disabled")
		}
		return t.Traversal.RoundTrip(withoutSyntheticBasicAuth(request))
	}
	direct := t.Direct
	if direct == nil {
		direct = http.DefaultTransport
	}
	return direct.RoundTrip(request)
}

// net/http treats the node ID in a gonzb+ice locator as URL userinfo and
// synthesizes Basic authorization before invoking a custom RoundTripper. The
// userinfo is a transport locator, not an HTTP credential. Remove only that
// exact synthesized value while preserving signed GoNZBNet authorization.
func withoutSyntheticBasicAuth(request *http.Request) *http.Request {
	if request == nil || request.URL == nil || request.URL.User == nil {
		return request
	}
	if _, hasPassword := request.URL.User.Password(); hasPassword {
		return request
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(request.URL.User.Username()+":"))
	if request.Header.Get("Authorization") != want {
		return request
	}
	clone := request.Clone(request.Context())
	clone.Header.Del("Authorization")
	return clone
}
