package nntp

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
)

// group-level metadata for scraping.
type GroupStats struct {
	Count int64
	Low   int64
	High  int64
	Group string
}

// overview header shape for XOVER scraping.
type OverviewHeader struct {
	ArticleNumber int64
	Subject       string
	Poster        string
	DateUTC       *time.Time
	MessageID     string
	References    string
	Bytes         int64
	Lines         int
	Xref          string
	RawOverview   map[string]any
}

// nntpConn pairs the high-level protocol helper with the raw network socket.
type nntpConn struct {
	tp  *textproto.Conn
	raw net.Conn
}

// Close ensures both layers are shut down.
func (c *nntpConn) Close() error {
	if c.tp != nil {
		c.tp.Close()
	}
	if c.raw != nil {
		return c.raw.Close()
	}
	return nil
}

type nntpProvider struct {
	conf config.ServerConfig
	pool chan *nntpConn
}

func NewNNTPProvider(c config.ServerConfig) Provider {
	return &nntpProvider{
		conf: c,
		pool: make(chan *nntpConn, c.MaxConnection),
	}
}

// Provider represents the contract for a Usenet server connection.
type Provider interface {
	ID() string
	Priority() int
	MaxConnection() int
	Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error)
	GroupStats(ctx context.Context, group string) (GroupStats, error)
	XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error)
	TestConnection() error
	Close() error
}

// Interface implimentation: ID
func (p *nntpProvider) ID() string { return p.conf.ID }

// Interface implimentation: Priority
func (p *nntpProvider) Priority() int { return p.conf.Priority }

// Interface implimentation: MaxConnection
func (p *nntpProvider) MaxConnection() int { return p.conf.MaxConnection }

func (p *nntpProvider) Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error) {
	conn, err := p.getConn()
	if err != nil {
		return nil, err
	}

	if len(groups) > 0 {
		if _, err := conn.tp.Cmd("GROUP %s", groups[0]); err != nil {
			p.returnConn(conn)
			return nil, err
		}
		if _, _, err := conn.tp.ReadCodeLine(211); err != nil {
			p.returnConn(conn)
			return nil, err
		}
	}

	formattedID := strings.TrimSpace(msgID)
	if !strings.HasPrefix(formattedID, "<") {
		formattedID = "<" + formattedID + ">"
	}

	// The BODY command tells the server to stream the article content
	if _, err := conn.tp.Cmd("BODY %s", formattedID); err != nil {
		p.returnConn(conn)
		return nil, err
	}

	// Expecting 222 Body follows
	code, msg, err := conn.tp.ReadCodeLine(222)
	if err != nil {
		if code == 430 || strings.Contains(strings.ToLower(msg), "no such article") {
			// If not found, we recycle the connection (it's still healthy)
			p.returnConn(conn)
			return nil, ErrArticleNotFound
		}
		conn.Close()
		return nil, fmt.Errorf("NNTP error %d: %s", code, msg)
	}

	// DotReader handles the NNTP "dot-stuffing" (terminating the stream with .\r\n)
	return &pooledReader{
		Reader: conn.tp.DotReader(),
		conn:   conn,
		p:      p,
	}, nil
}

// GroupStats issues GROUP and parses high/low/count.
func (p *nntpProvider) GroupStats(ctx context.Context, group string) (GroupStats, error) {
	if err := ctx.Err(); err != nil {
		return GroupStats{}, err
	}

	group = strings.TrimSpace(group)
	if group == "" {
		return GroupStats{}, fmt.Errorf("group is required")
	}

	conn, err := p.getConn()
	if err != nil {
		return GroupStats{}, err
	}
	defer p.returnConn(conn)

	stats, err := p.selectGroup(conn, group)
	if err != nil {
		return GroupStats{}, err
	}

	return stats, nil
}

// XOver reads overview rows for [from,to] after GROUP select.
func (p *nntpProvider) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if from <= 0 || to <= 0 || to < from {
		return nil, fmt.Errorf("invalid xover range %d-%d", from, to)
	}

	conn, err := p.getConn()
	if err != nil {
		return nil, err
	}

	// Keep a healthy connection in pool on success, close on protocol errors.
	returnConn := true
	defer func() {
		if returnConn {
			p.returnConn(conn)
		} else {
			conn.Close()
		}
	}()

	if _, err := p.selectGroup(conn, group); err != nil {
		return nil, err
	}

	if _, err := conn.tp.Cmd("XOVER %d-%d", from, to); err != nil {
		return nil, err
	}

	code, msg, err := conn.tp.ReadCodeLine(224)
	if err != nil {
		returnConn = false
		return nil, fmt.Errorf("XOVER failed (code %d): %s", code, msg)
	}

	dr := conn.tp.DotReader()
	sc := bufio.NewScanner(dr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	headers := make([]OverviewHeader, 0, to-from+1)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line := sc.Text()
		h, ok := parseOverviewLine(line)
		if !ok {
			continue
		}
		headers = append(headers, h)
	}

	if err := sc.Err(); err != nil {
		returnConn = false
		return nil, fmt.Errorf("read XOVER stream: %w", err)
	}

	return headers, nil
}

func (p *nntpProvider) selectGroup(conn *nntpConn, group string) (GroupStats, error) {
	if _, err := conn.tp.Cmd("GROUP %s", group); err != nil {
		return GroupStats{}, err
	}

	code, msg, err := conn.tp.ReadCodeLine(211)
	if err != nil {
		return GroupStats{}, fmt.Errorf("GROUP %s failed (code %d): %s", group, code, msg)
	}

	// 211 n f l s
	parts := strings.Fields(msg)
	if len(parts) < 4 {
		return GroupStats{}, fmt.Errorf("unexpected GROUP response for %s: %q", group, msg)
	}

	count, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return GroupStats{}, fmt.Errorf("parse GROUP count: %w", err)
	}
	low, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return GroupStats{}, fmt.Errorf("parse GROUP low: %w", err)
	}
	high, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return GroupStats{}, fmt.Errorf("parse GROUP high: %w", err)
	}

	return GroupStats{
		Count: count,
		Low:   low,
		High:  high,
		Group: group,
	}, nil
}

func parseOverviewLine(line string) (OverviewHeader, bool) {
	// RFC-style xover fields:
	// 0:number 1:subject 2:from 3:date 4:message-id 5:references 6:bytes 7:lines 8:xref(optional)
	fields := strings.Split(line, "\t")
	if len(fields) < 8 {
		return OverviewHeader{}, false
	}

	articleNumber, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
	if err != nil || articleNumber <= 0 {
		return OverviewHeader{}, false
	}

	bytesVal, _ := strconv.ParseInt(strings.TrimSpace(fields[6]), 10, 64)
	linesVal64, _ := strconv.ParseInt(strings.TrimSpace(fields[7]), 10, 64)

	dateUTC := parseNNTPDate(strings.TrimSpace(fields[3]))

	xref := ""
	if len(fields) > 8 {
		xref = strings.TrimSpace(fields[8])
	}

	raw := map[string]any{
		"line":       line,
		"references": strings.TrimSpace(fields[5]),
	}

	return OverviewHeader{
		ArticleNumber: articleNumber,
		Subject:       strings.TrimSpace(fields[1]),
		Poster:        strings.TrimSpace(fields[2]),
		DateUTC:       dateUTC,
		MessageID:     strings.TrimSpace(fields[4]),
		References:    strings.TrimSpace(fields[5]),
		Bytes:         bytesVal,
		Lines:         int(linesVal64),
		Xref:          xref,
		RawOverview:   raw,
	}, true
}

func parseNNTPDate(s string) *time.Time {
	if s == "" {
		return nil
	}

	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 -0700",
		time.RFC3339,
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			tt := t.UTC()
			return &tt
		}
	}

	return nil
}

func (p *nntpProvider) getConn() (*nntpConn, error) {
	select {
	case conn := <-p.pool:
		return conn, nil
	default:
		return p.dial()
	}
}

func (p *nntpProvider) returnConn(conn *nntpConn) {
	select {
	case p.pool <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close it
		conn.tp.Cmd("QUIT")
		conn.Close()
	}
}

func (p *nntpProvider) Close() error {
	close(p.pool)
	for conn := range p.pool {
		conn.tp.Cmd("QUIT")
		conn.Close()
	}
	return nil
}

func (p *nntpProvider) dial() (*nntpConn, error) {
	addr := net.JoinHostPort(p.conf.Host, strconv.Itoa(p.conf.Port))
	var netConn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if p.conf.TLS {
		tlsConfig := &tls.Config{
			ServerName: p.conf.Host,
			MinVersion: tls.VersionTLS12,
		}
		netConn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		netConn, err = dialer.Dial("tcp", addr)
	}

	if err != nil {
		return nil, err
	}

	conn := textproto.NewConn(netConn)

	// Ensure we close the connection if we return an error from this point on
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()

	code, msg, err := conn.ReadCodeLine(200)
	if tpErr, ok := err.(*textproto.Error); ok && tpErr.Code == 201 {
		// 201 is a valid NNTP greeting (no posting allowed).
		code = tpErr.Code
		msg = tpErr.Msg
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("NNTP greeting failed (code %d): %s", code, msg)
	}

	if err := p.authenticate(conn); err != nil {
		return nil, err
	}

	success = true
	return &nntpConn{
		tp:  conn,
		raw: netConn,
	}, nil
}

func (p *nntpProvider) authenticate(conn *textproto.Conn) error {
	if p.conf.Username == "" {
		return nil
	}

	if _, err := conn.Cmd("AUTHINFO USER %s", p.conf.Username); err != nil {
		return err
	}

	_, _, err := conn.ReadCodeLine(381) // 381: Password required
	if err != nil {
		return err
	}

	if _, err := conn.Cmd("AUTHINFO PASS %s", p.conf.Password); err != nil {
		return err
	}

	_, _, err = conn.ReadCodeLine(281) // 281: Authentication accepted
	return err
}

func (p *nntpProvider) TestConnection() error {
	conn, err := p.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err = conn.tp.Cmd("DATE"); err != nil {
		return fmt.Errorf("DATE command failed: %w", err)
	}

	code, msg, err := conn.tp.ReadCodeLine(111)
	if err != nil {
		return fmt.Errorf("auth check failed (code %d): %s", code, msg)
	}

	return nil
}

// pooledReader intercepts the EOF/Close to recycle the connection
type pooledReader struct {
	io.Reader
	conn *nntpConn
	p    *nntpProvider
}

func (pr *pooledReader) Read(b []byte) (n int, err error) {
	return pr.Reader.Read(b)
}

func (pr *pooledReader) Close() error {
	// Ensure we read the rest of the article before returning to pool
	pr.conn.raw.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := io.Copy(io.Discard, pr.Reader)
	pr.conn.raw.SetReadDeadline(time.Time{})

	if err != nil {
		pr.conn.tp.Close()
		return nil
	}

	pr.p.returnConn(pr.conn)
	return nil
}
