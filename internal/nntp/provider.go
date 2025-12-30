package nntp

import (
	"context"
	"crypto/tls"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/domain"
	"io"
	"net"
	"net/textproto"
	"strings"
	"time"
)

type nntpProvider struct {
	conf domain.ProviderConfig
	pool chan *textproto.Conn
}

func NewNNTPProvider(c config.ServerConfig) domain.Provider {
	return &nntpProvider{
		conf: domain.ProviderConfig{
			ID:            c.ID,
			Host:          c.Host,
			Port:          c.Port,
			Username:      c.Username,
			Password:      c.Password,
			TLS:           c.TLS,
			MaxConnection: c.MaxConnection,
			Priority:      c.Priority,
		},
		pool: make(chan *textproto.Conn, c.MaxConnection),
	}
}

// Interface implimentation: ID
func (p *nntpProvider) ID() string { return p.conf.ID }

// Interface implimentation: Priority
func (p *nntpProvider) Priority() int { return p.conf.Priority }

// Interface implimentation: MaxConnection
func (p *nntpProvider) MaxConnection() int { return p.conf.MaxConnection }

func (p *nntpProvider) Fetch(ctx context.Context, msgID string, groups []string) (io.Reader, error) {
	// Create a NEW connection for this specific fetch
	conn, err := p.getConn()
	if err != nil {
		return nil, err
	}

	if len(groups) > 0 {
		conn.Cmd("GROUP %s", groups[0])
		conn.ReadCodeLine(211)
	}

	formattedID := msgID
	if !strings.HasPrefix(formattedID, "<") {
		formattedID = "<" + formattedID + ">"
	}

	// The BODY command tells the server to stream the article content
	_, err = conn.Cmd("BODY %s", formattedID)
	if err != nil {
		p.returnConn(conn)
		return nil, err
	}

	// Expecting 222 Body follows
	code, msg, err := conn.ReadCodeLine(222)
	if err != nil {
		if code == 403 {
			// If not found, we recycle the connection (it's still healthy)
			p.returnConn(conn)
			return nil, fmt.Errorf("article not found (430): %s", formattedID)
		}
		conn.Close()
		return nil, fmt.Errorf("NNTP error %d: %s", code, msg)
	}

	// DotReader handles the NNTP "dot-stuffing" (terminating the stream with .\r\n)
	return &pooledReader{
		Reader: conn.DotReader(),
		conn:   conn,
		p:      p,
	}, nil
}

func (p *nntpProvider) getConn() (*textproto.Conn, error) {
	select {
	case conn := <-p.pool:
		// Check if connection is still alive by sending a NOOP or just returning it
		return conn, nil
	default:
		// Pool is empty, dial a new one
		return p.dial()
	}
}

func (p *nntpProvider) returnConn(conn *textproto.Conn) {
	select {
	case p.pool <- conn:
		// Successfully returned to pool
	default:
		// Pool is full (shouldn't happen with our Semaphore), close it
		conn.Cmd("QUIT")
		conn.Close()
	}
}

func (p *nntpProvider) Close() error {
	close(p.pool)
	for conn := range p.pool {
		conn.Cmd("QUIT")
		conn.Close()
	}
	return nil
}

func (p *nntpProvider) dial() (*textproto.Conn, error) {
	addr := fmt.Sprintf("%s:%d", p.conf.Host, p.conf.Port)
	var netConn net.Conn
	var err error

	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if p.conf.TLS {
		tlsConfig := &tls.Config{
			ServerName: p.conf.Host,
			MinVersion: tls.VersionTLS12,
			RootCAs:    nil,
		}

		netConn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		netConn, err = dialer.Dial("tcp", addr)
	}

	if err != nil {
		return nil, err
	}

	conn := textproto.NewConn(netConn)
	_, _, err = conn.ReadCodeLine(200)
	if err != nil {
		// Some servers return 201 (Ready, no posting allowed)
		_, _, err = conn.ReadCodeLine(201)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}

	if err := p.authenticate(conn); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

func (p *nntpProvider) authenticate(conn *textproto.Conn) error {

	if p.conf.Username == "" {
		return nil
	}

	// AUTHINFO USER
	if _, err := conn.Cmd("AUTHINFO USER %s", p.conf.Username); err != nil {
		return err
	}

	_, _, err := conn.ReadCodeLine(381) // 381: Password required
	if err != nil {
		return err
	}

	// AUTHINFO PASS
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

	// Send a 'HELP' or 'DATE' command to verify we are truly authenticated and active
	_, err = conn.Cmd("DATE")
	if err != nil {
		return fmt.Errorf("DATE command failed: %w", err)
	}

	code, msg, err := conn.ReadCodeLine(111) // 111 is the success code for DATE
	if err != nil {
		return fmt.Errorf("auth check failed (code %d): %s", code, msg)
	}

	return nil
}

// pooledReader intercepts the EOF/Close to recycle the connection
type pooledReader struct {
	io.Reader
	conn *textproto.Conn
	p    *nntpProvider
}

func (pr *pooledReader) Read(b []byte) (n int, err error) {
	n, err = pr.Reader.Read(b)
	return n, err
}

// Close is called by the Service worker via 'defer closer.Close()'
func (pr *pooledReader) Close() error {
	_, err := io.Copy(io.Discard, pr.Reader)
	if err != nil {
		pr.conn.Close()
		return err
	}

	pr.p.returnConn(pr.conn)
	return nil
}
