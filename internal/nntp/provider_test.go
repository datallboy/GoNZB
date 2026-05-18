package nntp

import (
	"context"
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
	if _, ok := got.RawOverview["line"]; ok {
		t.Fatalf("expected raw overview to omit full XOVER line, got %#v", got.RawOverview)
	}
}

func TestParseOverviewLinePreservesPosterAndMessageIDPartCounter(t *testing.T) {
	line := "2390174988\tMicrosoft Office Pro Plus 2021-2024 v2604 Build 19929.20106 (x64) Incl. Activator - [23/35] - \"Microsoft Office Pro Plus 2021-2024 v2604 Build 19929.20106 (x64) Incl. Activator.part21.rar\" yEnc (84/700)\tboob@mail.com (boobus)\tSat, 09 May 2026 10:08:11 GMT\t<Part84of700.88B60C1037DB48589E2DC79BE09F92DA@1778298129.local>\t\t399016\t3062"

	got, ok := parseOverviewLine(line)
	if !ok {
		t.Fatal("expected overview line to parse")
	}
	if got.ArticleNumber != 2390174988 {
		t.Fatalf("expected article number 2390174988, got %d", got.ArticleNumber)
	}
	if got.Poster != "boob@mail.com (boobus)" {
		t.Fatalf("expected poster to be preserved, got %q", got.Poster)
	}
	if got.MessageID != "<Part84of700.88B60C1037DB48589E2DC79BE09F92DA@1778298129.local>" {
		t.Fatalf("expected message-id to be preserved, got %q", got.MessageID)
	}
	if got.Bytes != 399016 || got.Lines != 3062 {
		t.Fatalf("expected bytes/lines 399016/3062, got %d/%d", got.Bytes, got.Lines)
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

func TestGroupStatsWithConnClosesBrokenConnection(t *testing.T) {
	client, server := net.Pipe()
	server.Close()

	p := &nntpProvider{
		conf: config.ServerConfig{ID: "test", MaxConnection: 1},
		pool: make(chan *nntpConn, 1),
	}
	conn := &nntpConn{
		tp:  textproto.NewConn(client),
		raw: client,
	}

	_, retry, err := p.groupStatsWithConn(conn, "alt.binaries.test")
	if err == nil {
		t.Fatal("expected group stats error")
	}
	if !retry {
		t.Fatal("expected broken connection to be retryable")
	}
	if got := len(p.pool); got != 0 {
		t.Fatalf("expected broken connection to stay out of pool, got %d", got)
	}
}

func TestXOverWithConnClosesBrokenConnection(t *testing.T) {
	client, server := net.Pipe()
	server.Close()

	p := &nntpProvider{
		conf: config.ServerConfig{ID: "test", MaxConnection: 1},
		pool: make(chan *nntpConn, 1),
	}
	conn := &nntpConn{
		tp:  textproto.NewConn(client),
		raw: client,
	}

	_, retry, err := p.xoverWithConn(context.Background(), conn, "alt.binaries.test", 1, 10)
	if err == nil {
		t.Fatal("expected xover error")
	}
	if !retry {
		t.Fatal("expected broken connection to be retryable")
	}
	if got := len(p.pool); got != 0 {
		t.Fatalf("expected broken connection to stay out of pool, got %d", got)
	}
}

func TestReturnConnClosesConnectionPastMaxAge(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	p := &nntpProvider{
		conf: config.ServerConfig{ID: "test", MaxConnection: 1, PoolMaxAgeSeconds: 1},
		pool: make(chan *nntpConn, 1),
	}
	conn := &nntpConn{
		tp:         textproto.NewConn(client),
		raw:        client,
		createdAt:  time.Now().Add(-2 * time.Second),
		lastUsedAt: time.Now(),
	}

	p.returnConn(conn)

	if got := len(p.pool); got != 0 {
		t.Fatalf("expected aged connection to stay out of pool, got %d", got)
	}
}

func TestGetConnDiscardsIdlePooledConnection(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	p := &nntpProvider{
		conf: config.ServerConfig{
			ID:                     "test",
			Host:                   "127.0.0.1",
			Port:                   563,
			MaxConnection:          1,
			PoolIdleTimeoutSeconds: 1,
		},
		pool: make(chan *nntpConn, 1),
	}
	stale := &nntpConn{
		tp:         textproto.NewConn(client),
		raw:        client,
		createdAt:  time.Now().Add(-5 * time.Second),
		lastUsedAt: time.Now().Add(-3 * time.Second),
	}
	p.pool <- stale

	got, err := p.getConn()
	if err == nil {
		got.Close()
		t.Fatal("expected dial failure after stale pooled connection was discarded")
	}
	if got != nil {
		t.Fatalf("expected nil connection, got %+v", got)
	}
	if gotPool := len(p.pool); gotPool != 0 {
		t.Fatalf("expected stale connection to be drained from pool, got %d", gotPool)
	}
}
