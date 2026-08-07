package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyInjectsFailuresThenForwards(t *testing.T) {
	var forwardedBody string
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read forwarded body: %v", err)
		}
		forwardedBody = string(data)
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"created":true}`))
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	p := &proxy{target: targetURL, failures: 2}

	for attempt := 1; attempt <= 3; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/uploader/submissions", strings.NewReader("synthetic"))
		response := httptest.NewRecorder()
		p.ServeHTTP(response, request)
		want := http.StatusServiceUnavailable
		if attempt == 3 {
			want = http.StatusCreated
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.Code, want)
		}
	}

	if forwardedBody != "synthetic" || p.requests.Load() != 3 || p.failed.Load() != 2 || p.forwarded.Load() != 1 {
		t.Fatalf("unexpected proxy state body=%q requests=%d failed=%d forwarded=%d",
			forwardedBody, p.requests.Load(), p.failed.Load(), p.forwarded.Load())
	}
}
