package nntp

import (
	"net"
	"net/textproto"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
)

func TestParseNNTPDateSupportsTwoDigitYearUTC(t *testing.T) {
	got := parseNNTPDate("Thu, 09 Apr 26 18:13:57 UTC")
	if got == nil {
		t.Fatal("expected parsed date, got nil")
	}

	want := time.Date(2026, time.April, 9, 18, 13, 57, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.Format(time.RFC3339))
	}
}

func TestParseOverviewLineParsesTwoDigitYearDate(t *testing.T) {
	line := "1881650125\tSubject Here\tposter@example\tThu, 09 Apr 26 18:13:57 UTC\t<message@example>\t\t740410\t0\tXref: news.easynews.com alt.binaries.sleazemovies:1881650125"

	got, ok := parseOverviewLine(line)
	if !ok {
		t.Fatal("expected overview line to parse")
	}
	if got.DateUTC == nil {
		t.Fatal("expected parsed overview date, got nil")
	}

	want := time.Date(2026, time.April, 9, 18, 13, 57, 0, time.UTC)
	if !got.DateUTC.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.DateUTC.Format(time.RFC3339))
	}
}

func TestReturnConnAfterCloseDoesNotPanic(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	p := &nntpProvider{
		conf: config.ServerConfig{ID: "test", MaxConnection: 1},
		pool: make(chan *nntpConn, 1),
	}
	conn := &nntpConn{
		tp:  textproto.NewConn(client),
		raw: client,
	}

	if err := p.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("returnConn panicked after provider close: %v", r)
		}
	}()

	p.returnConn(conn)
}

func TestGetConnAfterCloseReturnsError(t *testing.T) {
	p := &nntpProvider{
		conf: config.ServerConfig{ID: "test", MaxConnection: 1},
		pool: make(chan *nntpConn, 1),
	}

	if err := p.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}

	if _, err := p.getConn(); err == nil {
		t.Fatal("expected getConn to fail after provider close")
	}
}

func TestIsRecoverableConnError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "broken pipe", err: net.ErrClosed, want: true},
		{name: "timeout text", err: &net.DNSError{Err: "i/o timeout"}, want: true},
		{name: "plain error", err: textproto.ProtocolError("bad response"), want: false},
	}

	for _, tc := range cases {
		if got := isRecoverableConnError(tc.err); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, got)
		}
	}
}
