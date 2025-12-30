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
	conn *textproto.Conn
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
	}
}

// Interface implimentation: ID
func (p *nntpProvider) ID() string { return p.conf.ID }

// Interface implimentation: Priority
func (p *nntpProvider) Priority() int { return p.conf.Priority }

// Interface implimentation: MaxConnection
func (p *nntpProvider) MaxConnection() int { return p.conf.MaxConnection }

func (p *nntpProvider) Fetch(ctx context.Context, msgID string) (io.Reader, error) {
	// Ensure we are connected and authenticated
	if err := p.ensureConnected(); err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	formattedID := msgID
	if !strings.HasPrefix(formattedID, "<") {
		formattedID = "<" + formattedID + ">"
	}

	// The BODY command tells the server to stream the article content
	_, err := p.conn.Cmd("BODY <%s>", formattedID)
	if err != nil {
		return nil, err
	}

	// Expecting 222 Body follows
	_, _, err = p.conn.ReadCodeLine(222)
	if err != nil {
		return nil, err
	}

	// DotReader handles the NNTP "dot-stuffing" (terminating the stream with .\r\n)
	return p.conn.DotReader(), nil
}

func (p *nntpProvider) Close() error {
	if p.conn != nil {
		// Send the NNTP QUIT command so the server can release
		// the connection slot immediately.
		p.conn.Cmd("QUIT")
		return p.conn.Close()
	}
	return nil
}

// handle connection and auth
func (p *nntpProvider) ensureConnected() error {
	if p.conn != nil {
		return nil // already connected
	}

	addr := fmt.Sprintf("%s:%d", p.conf.Host, p.conf.Port)

	var conn io.ReadWriteCloser
	var err error

	if p.conf.TLS {
		tlsConfig := &tls.Config{
			ServerName: p.conf.Host,
			MinVersion: tls.VersionTLS12,
			RootCAs:    nil,
		}

		conn, err = tls.Dial("tcp", addr, tlsConfig)
	} else {
		// Fallback for non-SSL ports
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}

	p.conn = textproto.NewConn(conn)

	// Usenet servers usually green with a 200 or 201
	_, _, err = p.conn.ReadCodeLine(200)
	if err != nil {
		// Fallback to 201 (posting not allowed, but fine for downloading)
		_, _, err = p.conn.ReadCodeLine(201)
		if err != nil {
			return err
		}
	}

	return p.authenticate()

}

func (p *nntpProvider) authenticate() error {

	if p.conf.Username == "" {
		return nil
	}

	// AUTHINFO USER
	if _, err := p.conn.Cmd("AUTHINFO USER %s", p.conf.Username); err != nil {
		return err
	}

	_, _, err := p.conn.ReadCodeLine(381) // 381: Password required
	if err != nil {
		return err
	}

	// AUTHINFO PASS
	if _, err := p.conn.Cmd("AUTHINFO PASS %s", p.conf.Password); err != nil {
		return err
	}

	_, _, err = p.conn.ReadCodeLine(281) // 281: Authentication accepted

	return err
}
