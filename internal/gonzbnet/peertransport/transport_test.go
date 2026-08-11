package peertransport

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type markerTransport struct{ err error }

func (m markerTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, m.err }

func TestRoutesByScheme(t *testing.T) {
	directErr := errors.New("direct")
	iceErr := errors.New("ice")
	transport := Transport{Direct: markerTransport{directErr}, Traversal: markerTransport{iceErr}}
	for raw, want := range map[string]error{"https://peer.example/path": directErr, "gonzb+ice://node_x@connect.example/path": iceErr} {
		request, _ := http.NewRequest(http.MethodGet, raw, nil)
		_, got := transport.RoundTrip(request)
		if !errors.Is(got, want) {
			t.Fatalf("%s routed to %v, want %v", raw, got, want)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestTraversalRemovesNetHTTPSyntheticBasicAuthorization(t *testing.T) {
	var authorization string
	client := &http.Client{Transport: Transport{Traversal: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		authorization = request.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}}
	response, err := client.Get("gonzb+ice://node_x@connect.example/gonzbnet/v1/node")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if authorization != "" {
		t.Fatalf("synthetic authorization reached traversal transport: %q", authorization)
	}
}

func TestTraversalPreservesSignedAuthorization(t *testing.T) {
	const signed = `GoNZBNet node_id="node_x",timestamp="now",nonce="nonce",signature="signature"`
	var authorization string
	client := &http.Client{Transport: Transport{Traversal: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		authorization = request.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}}
	request, err := http.NewRequest(http.MethodGet, "gonzb+ice://node_x@connect.example/gonzbnet/v1/outbox", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", signed)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if authorization != signed {
		t.Fatalf("signed authorization = %q, want %q", authorization, signed)
	}
}
