package nntp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	tp         *textproto.Conn
	raw        net.Conn
	createdAt  time.Time
	lastUsedAt time.Time
}

type providerLogger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
}

type poolDiscardReason string

const (
	discardIdle  poolDiscardReason = "idle"
	discardAge   poolDiscardReason = "age"
	discardError poolDiscardReason = "error"
)

type providerStats struct {
	dials             atomic.Int64
	dialFailures      atomic.Int64
	poolReuses        atomic.Int64
	poolReturns       atomic.Int64
	poolDiscardIdle   atomic.Int64
	poolDiscardAge    atomic.Int64
	poolDiscardError  atomic.Int64
	fetchRetries      atomic.Int64
	groupStatsRetries atomic.Int64
	xoverRetries      atomic.Int64
	recoverableErrors atomic.Int64
}

type providerStatsSnapshot struct {
	Dials             int64
	DialFailures      int64
	PoolReuses        int64
	PoolReturns       int64
	PoolDiscardIdle   int64
	PoolDiscardAge    int64
	PoolDiscardError  int64
	FetchRetries      int64
	GroupStatsRetries int64
	XOverRetries      int64
	RecoverableErrors int64
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
	mu   sync.RWMutex
	done bool

	log          providerLogger
	stats        providerStats
	statsLogMu   sync.Mutex
	lastStatsLog time.Time
}

func NewNNTPProvider(c config.ServerConfig) Provider {
	return NewNNTPProviderWithLogger(c, nil)
}

func NewNNTPProviderWithLogger(c config.ServerConfig, log providerLogger) Provider {
	if !c.EnablePoolLogging {
		log = nil
	}
	return &nntpProvider{
		conf: c,
		pool: make(chan *nntpConn, c.MaxConnection),
		log:  log,
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
	formattedID := strings.TrimSpace(msgID)
	if !strings.HasPrefix(formattedID, "<") {
		formattedID = "<" + formattedID + ">"
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := p.getConn()
		if err != nil {
			return nil, err
		}

		reader, retry, err := p.fetchWithConn(ctx, conn, formattedID, groups)
		if err == nil {
			return reader, nil
		}
		lastErr = err
		if retry && attempt == 0 {
			p.stats.fetchRetries.Add(1)
			p.logRecoverableRetry("fetch", err, formattedID)
			continue
		}
		return nil, err
	}

	return nil, lastErr
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

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := p.getConn()
		if err != nil {
			return GroupStats{}, err
		}

		stats, retry, err := p.groupStatsWithConn(conn, group)
		if err == nil {
			return stats, nil
		}
		lastErr = err
		if retry && attempt == 0 {
			p.stats.groupStatsRetries.Add(1)
			p.logRecoverableRetry("group_stats", err, group)
			continue
		}
		return GroupStats{}, err
	}

	return GroupStats{}, lastErr
}

// XOver reads overview rows for [from,to] after GROUP select.
func (p *nntpProvider) XOver(ctx context.Context, group string, from, to int64) ([]OverviewHeader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if from <= 0 || to <= 0 || to < from {
		return nil, fmt.Errorf("invalid xover range %d-%d", from, to)
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := p.getConn()
		if err != nil {
			return nil, err
		}

		headers, retry, err := p.xoverWithConn(ctx, conn, group, from, to)
		if err == nil {
			return headers, nil
		}
		lastErr = err
		if retry && attempt == 0 {
			p.stats.xoverRetries.Add(1)
			p.logRecoverableRetry("xover", err, fmt.Sprintf("%s:%d-%d", group, from, to))
			continue
		}
		return nil, err
	}

	return nil, lastErr
}

func (p *nntpProvider) groupStatsWithConn(conn *nntpConn, group string) (GroupStats, bool, error) {
	stats, err := p.selectGroup(conn, group)
	if err != nil {
		conn.Close()
		return GroupStats{}, isRecoverableConnError(err), err
	}

	p.returnConn(conn)
	return stats, false, nil
}

func (p *nntpProvider) xoverWithConn(ctx context.Context, conn *nntpConn, group string, from, to int64) ([]OverviewHeader, bool, error) {
	if _, err := p.selectGroup(conn, group); err != nil {
		conn.Close()
		return nil, isRecoverableConnError(err), err
	}

	if _, err := conn.tp.Cmd("XOVER %d-%d", from, to); err != nil {
		conn.Close()
		return nil, isRecoverableConnError(err), err
	}

	code, msg, err := conn.tp.ReadCodeLine(224)
	if err != nil {
		conn.Close()
		return nil, isRecoverableConnError(err), fmt.Errorf("XOVER failed (code %d): %s", code, msg)
	}

	dr := conn.tp.DotReader()
	sc := bufio.NewScanner(dr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	headers := make([]OverviewHeader, 0, to-from+1)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			conn.Close()
			return nil, false, err
		}

		line := sc.Text()
		h, ok := parseOverviewLine(line)
		if !ok {
			continue
		}
		headers = append(headers, h)
	}

	if err := sc.Err(); err != nil {
		conn.Close()
		return nil, isRecoverableConnError(err), fmt.Errorf("read XOVER stream: %w", err)
	}

	p.returnConn(conn)
	return headers, false, nil
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
		"Mon, 02 Jan 06 15:04:05 MST",
		"Mon, 2 Jan 06 15:04:05 MST",
		"Mon, 02 Jan 06 15:04:05 -0700",
		"Mon, 2 Jan 06 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2 Jan 06 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700",
		"2 Jan 06 15:04:05 -0700",
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
	p.mu.RLock()
	done := p.done
	p.mu.RUnlock()
	if done {
		return nil, fmt.Errorf("provider %s is closed", p.conf.ID)
	}

	now := time.Now()
	for {
		select {
		case conn := <-p.pool:
			if conn == nil {
				return p.dial()
			}
			if discard, expired := p.shouldDiscardConn(conn, now); expired {
				p.discardConn(conn, discard)
				continue
			}
			p.stats.poolReuses.Add(1)
			return conn, nil
		default:
			return p.dial()
		}
	}
}

func (p *nntpProvider) returnConn(conn *nntpConn) {
	if conn == nil {
		return
	}

	now := time.Now()
	conn.lastUsedAt = now
	if discard, expired := p.shouldDiscardConn(conn, now); expired {
		p.discardConn(conn, discard)
		return
	}

	p.mu.RLock()
	done := p.done
	p.mu.RUnlock()
	if done {
		p.closeConn(conn)
		return
	}

	select {
	case p.pool <- conn:
		p.stats.poolReturns.Add(1)
	default:
		p.discardConn(conn, discardError)
	}

	p.maybeLogStats()
}

func (p *nntpProvider) Close() error {
	p.mu.Lock()
	if p.done {
		p.mu.Unlock()
		return nil
	}
	p.done = true
	p.mu.Unlock()

	for {
		select {
		case conn := <-p.pool:
			if conn != nil {
				p.discardConn(conn, discardError)
			}
		default:
			p.logStats("provider_close")
			return nil
		}
	}
}

func (p *nntpProvider) closeConn(conn *nntpConn) {
	if conn == nil {
		return
	}
	_ = conn.Close()
}

func (p *nntpProvider) shouldDiscardConn(conn *nntpConn, now time.Time) (poolDiscardReason, bool) {
	if conn == nil {
		return discardError, true
	}

	if maxAge := p.poolMaxAge(); maxAge > 0 && !conn.createdAt.IsZero() && now.Sub(conn.createdAt) > maxAge {
		return discardAge, true
	}

	lastUsedAt := conn.lastUsedAt
	if lastUsedAt.IsZero() {
		lastUsedAt = conn.createdAt
	}
	if idleTimeout := p.poolIdleTimeout(); idleTimeout > 0 && !lastUsedAt.IsZero() && now.Sub(lastUsedAt) > idleTimeout {
		return discardIdle, true
	}

	return "", false
}

func (p *nntpProvider) discardConn(conn *nntpConn, reason poolDiscardReason) {
	switch reason {
	case discardIdle:
		p.stats.poolDiscardIdle.Add(1)
	case discardAge:
		p.stats.poolDiscardAge.Add(1)
	default:
		p.stats.poolDiscardError.Add(1)
	}
	p.closeConn(conn)
	p.maybeLogStats()
}

func (p *nntpProvider) fetchWithConn(ctx context.Context, conn *nntpConn, formattedID string, groups []string) (io.Reader, bool, error) {
	if len(groups) > 0 {
		if _, err := conn.tp.Cmd("GROUP %s", groups[0]); err != nil {
			conn.Close()
			return nil, isRecoverableConnError(err), err
		}
		if _, _, err := conn.tp.ReadCodeLine(211); err != nil {
			conn.Close()
			return nil, isRecoverableConnError(err), err
		}
	}

	if _, err := conn.tp.Cmd("BODY %s", formattedID); err != nil {
		conn.Close()
		return nil, isRecoverableConnError(err), err
	}

	code, msg, err := conn.tp.ReadCodeLine(222)
	if err != nil {
		if code == 430 || strings.Contains(strings.ToLower(msg), "no such article") {
			p.returnConn(conn)
			return nil, false, ErrArticleNotFound
		}
		conn.Close()
		if isRecoverableConnError(err) {
			return nil, true, err
		}
		return nil, false, fmt.Errorf("NNTP error %d: %s", code, msg)
	}

	return &pooledReader{
		Reader: conn.tp.DotReader(),
		conn:   conn,
		p:      p,
		ctx:    ctx,
	}, false, nil
}

func isRecoverableConnError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	text := strings.ToLower(err.Error())
	for _, needle := range []string{
		"broken pipe",
		"closed pipe",
		"connection reset",
		"connection refused",
		"unexpected eof",
		"timeout",
		"i/o timeout",
		"tls:",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func (p *nntpProvider) dial() (*nntpConn, error) {
	addr := net.JoinHostPort(p.conf.Host, strconv.Itoa(p.conf.Port))
	var netConn net.Conn
	var err error

	dialer := &net.Dialer{
		Timeout:   p.dialTimeout(),
		KeepAlive: p.tcpKeepAlivePeriod(),
	}

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
		p.stats.dialFailures.Add(1)
		p.maybeLogStats()
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
		p.stats.dialFailures.Add(1)
		p.maybeLogStats()
		return nil, fmt.Errorf("NNTP greeting failed (code %d): %s", code, msg)
	}

	if err := p.authenticate(conn); err != nil {
		p.stats.dialFailures.Add(1)
		p.maybeLogStats()
		return nil, err
	}

	now := time.Now()
	success = true
	p.stats.dials.Add(1)
	p.maybeLogStats()
	return &nntpConn{
		tp:         conn,
		raw:        netConn,
		createdAt:  now,
		lastUsedAt: now,
	}, nil
}

func (p *nntpProvider) dialTimeout() time.Duration {
	seconds := p.conf.DialTimeoutSeconds
	if seconds <= 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func (p *nntpProvider) tcpKeepAlivePeriod() time.Duration {
	seconds := p.conf.TCPKeepAliveSeconds
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func (p *nntpProvider) poolIdleTimeout() time.Duration {
	seconds := p.conf.PoolIdleTimeoutSeconds
	if seconds <= 0 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second
}

func (p *nntpProvider) poolMaxAge() time.Duration {
	seconds := p.conf.PoolMaxAgeSeconds
	if seconds <= 0 {
		seconds = 600
	}
	return time.Duration(seconds) * time.Second
}

func (p *nntpProvider) logRecoverableRetry(op string, err error, target string) {
	p.stats.recoverableErrors.Add(1)
	if p.log != nil {
		p.log.Warn("nntp provider=%s recoverable %s retry target=%s err=%v", p.conf.ID, op, target, err)
	}
	p.maybeLogStats()
}

func (p *nntpProvider) maybeLogStats() {
	if p.log == nil {
		return
	}

	p.statsLogMu.Lock()
	defer p.statsLogMu.Unlock()

	now := time.Now()
	if !p.lastStatsLog.IsZero() && now.Sub(p.lastStatsLog) < 60*time.Second {
		return
	}
	p.lastStatsLog = now
	p.logStatsLocked("periodic")
}

func (p *nntpProvider) logStats(reason string) {
	if p.log == nil {
		return
	}
	p.statsLogMu.Lock()
	defer p.statsLogMu.Unlock()
	p.lastStatsLog = time.Now()
	p.logStatsLocked(reason)
}

func (p *nntpProvider) logStatsLocked(reason string) {
	if p.log == nil {
		return
	}
	s := p.statsSnapshot()
	p.log.Info(
		"nntp pool provider=%s reason=%s dials=%d dial_failures=%d reuses=%d returns=%d discard_idle=%d discard_age=%d discard_error=%d fetch_retries=%d group_retries=%d xover_retries=%d recoverable_errors=%d idle_timeout=%s max_age=%s keepalive=%s",
		p.conf.ID,
		reason,
		s.Dials,
		s.DialFailures,
		s.PoolReuses,
		s.PoolReturns,
		s.PoolDiscardIdle,
		s.PoolDiscardAge,
		s.PoolDiscardError,
		s.FetchRetries,
		s.GroupStatsRetries,
		s.XOverRetries,
		s.RecoverableErrors,
		p.poolIdleTimeout(),
		p.poolMaxAge(),
		p.tcpKeepAlivePeriod(),
	)
}

func (p *nntpProvider) statsSnapshot() providerStatsSnapshot {
	return providerStatsSnapshot{
		Dials:             p.stats.dials.Load(),
		DialFailures:      p.stats.dialFailures.Load(),
		PoolReuses:        p.stats.poolReuses.Load(),
		PoolReturns:       p.stats.poolReturns.Load(),
		PoolDiscardIdle:   p.stats.poolDiscardIdle.Load(),
		PoolDiscardAge:    p.stats.poolDiscardAge.Load(),
		PoolDiscardError:  p.stats.poolDiscardError.Load(),
		FetchRetries:      p.stats.fetchRetries.Load(),
		GroupStatsRetries: p.stats.groupStatsRetries.Load(),
		XOverRetries:      p.stats.xoverRetries.Load(),
		RecoverableErrors: p.stats.recoverableErrors.Load(),
	}
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
	ctx  context.Context
}

func (pr *pooledReader) Read(b []byte) (n int, err error) {
	for {
		if pr.ctx != nil {
			if err := pr.ctx.Err(); err != nil {
				pr.conn.Close()
				return 0, err
			}
			_ = pr.conn.raw.SetReadDeadline(time.Now().Add(1 * time.Second))
		}

		n, err = pr.Reader.Read(b)
		if pr.ctx != nil {
			_ = pr.conn.raw.SetReadDeadline(time.Time{})
		}
		if err == nil {
			return n, nil
		}

		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if pr.ctx != nil && pr.ctx.Err() != nil {
				pr.conn.Close()
				return 0, pr.ctx.Err()
			}
			continue
		}

		return n, err
	}
}

func (pr *pooledReader) Close() error {
	// Ensure we read the rest of the article before returning to pool
	pr.conn.raw.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := io.Copy(io.Discard, pr.Reader)
	pr.conn.raw.SetReadDeadline(time.Time{})

	if err != nil {
		pr.conn.Close()
		return nil
	}

	pr.p.returnConn(pr.conn)
	return nil
}
