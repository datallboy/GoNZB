package wiring

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/datallboy/gonzb/internal/infra/config"
)

type directObservation struct {
	locator              string
	success              bool
	bytesSent, bytesRecv int64
}

type directObservationStore struct{ items chan directObservation }

func (s *directObservationStore) RecordFederationEndpointResultByLocator(_ context.Context, locator string, success bool, _ int64, bytesSent, bytesReceived int64, _ string) error {
	s.items <- directObservation{locator: locator, success: success, bytesSent: bytesSent, bytesRecv: bytesReceived}
	return nil
}

type directRoundTripper func(*http.Request) (*http.Response, error)

func (fn directRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestObservedDirectTransportUsesFederationBaseLocator(t *testing.T) {
	store := &directObservationStore{items: make(chan directObservation, 1)}
	transport := observedFederationDirectTransport{
		basePath: "/gonzbnet/v1",
		store:    store,
		base: directRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("result")), ContentLength: 6}, nil
		}),
	}
	request, _ := http.NewRequest(http.MethodPost, "https://node.example/gonzbnet/v1/inbox", strings.NewReader("event"))
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	item := <-store.items
	if item.locator != "https://node.example/gonzbnet/v1" || !item.success || item.bytesSent != 5 || item.bytesRecv != 6 {
		t.Fatalf("unexpected direct observation: %+v", item)
	}
}

func TestTraversalBodyLimitIncludesBatchesAndHardCap(t *testing.T) {
	if got, want := traversalMaxBodyBytes(config.GoNZBNetConfig{}), int64(100*(256<<10)+(64<<10)); got != want {
		t.Fatalf("default traversal body limit = %d, want %d", got, want)
	}
	if got := traversalMaxBodyBytes(config.GoNZBNetConfig{MaxEventBytes: 1 << 30, MaxBatchEvents: 1000}); got != 128<<20 {
		t.Fatalf("hard traversal body limit = %d", got)
	}
}

func TestConfiguredCoordinatorHosts(t *testing.T) {
	hosts := configuredCoordinatorHosts([]config.GoNZBNetCoordinatorConfig{{URL: "https://Connect.Example:8443/prefix"}, {URL: "invalid"}})
	if len(hosts) != 1 || hosts[0] != "connect.example:8443" {
		t.Fatalf("coordinator hosts = %v", hosts)
	}
}
