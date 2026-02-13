package nntp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
)

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

	formattedID := msgID
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
	addr := fmt.Sprintf("%s:%d", p.conf.Host, p.conf.Port)
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
	if err != nil {
		// Some servers return 201 (Ready, no posting allowed)
		code, msg, err = conn.ReadCodeLine(201)
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
