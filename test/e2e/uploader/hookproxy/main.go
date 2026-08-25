// Command hookproxy injects deterministic transient HTTP failures before
// forwarding Postie's uploader hook to a local GoNZB node.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type proxy struct {
	target    *url.URL
	failures  int64
	requests  atomic.Int64
	failed    atomic.Int64
	forwarded atomic.Int64
}

func main() {
	address := flag.String("listen", "127.0.0.1:18091", "loopback listen address")
	targetValue := flag.String("target", "http://127.0.0.1:18081", "target base URL")
	failures := flag.Int64("failures", 2, "number of initial 503 responses")
	readyPath := flag.String("ready-file", "", "file written before serving")
	flag.Parse()

	target, err := url.Parse(*targetValue)
	if err != nil {
		panic(err)
	}
	p := &proxy{target: target, failures: *failures}
	server := &http.Server{Addr: *address, Handler: p, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		panic(err)
	}
	if *readyPath != "" {
		if err := os.WriteFile(*readyPath, []byte(listener.Addr().String()+"\n"), 0o600); err != nil {
			panic(err)
		}
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}

func (p *proxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/stats" {
		_ = json.NewEncoder(writer).Encode(map[string]int64{
			"requests": p.requests.Load(), "injected_failures": p.failed.Load(), "forwarded": p.forwarded.Load(),
		})
		return
	}

	requestNumber := p.requests.Add(1)
	if requestNumber <= p.failures {
		p.failed.Add(1)
		http.Error(writer, "synthetic transient uploader outage", http.StatusServiceUnavailable)
		return
	}

	target := *p.target
	target.Path = strings.TrimSuffix(target.Path, "/") + request.URL.Path
	target.RawQuery = request.URL.RawQuery
	forward, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), request.Body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	forward.Header = request.Header.Clone()
	response, err := http.DefaultClient.Do(forward)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	p.forwarded.Add(1)
	for name, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}
