package nntp

import (
	"crypto/tls"
	"fmt"
	"gonzb/internal/config"
	"io"
	"net/textproto"
)

type Repository struct {
	addr     string // "news.example.com:563"
	hostname string // "news.example.com"
	user     string
	pass     string
	conn     *textproto.Conn
}

func NewRepository(cfg config.ServerConfig) *Repository {
	return &Repository{
		addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		hostname: cfg.Host,
		user:     cfg.Username,
		pass:     cfg.Password,
	}
}

func (r *Repository) Authenticate() error {

	// Excplicity using tls.Dial ensures the TCP handshake
	// is immediately followed by a TLS handshake.
	tlsConfig := &tls.Config{
		ServerName: r.hostname,
	}

	conn, err := tls.Dial("tcp", r.addr, tlsConfig)
	if err != nil {
		return err
	}
	r.conn = textproto.NewConn(conn)

	// Usenet servers usually greet with a 200
	_, _, err = r.conn.ReadCodeLine(200)
	if err != nil {
		return fmt.Errorf("initial connection failed: %w", err)
	}

	// AUTHINFO USER
	if _, err := r.conn.Cmd("AUTHINFO USER %s", r.user); err != nil {
		return err
	}

	// AUTHINFO PASS
	if _, err := r.conn.Cmd("AUTHINFO PASS %s", r.pass); err != nil {
		return err
	}

	return nil
}

func (r *Repository) FetchBody(messageID string) (io.Reader, error) {
	// The BODY command tells the server to stream the article content
	id, err := r.conn.Cmd("BODY <%s>", messageID)
	if err != nil {
		return nil, err
	}

	r.conn.StartResponse(id)
	defer r.conn.EndResponse(id)

	// Expecting 222 Body follkows
	_, _, err = r.conn.ReadCodeLine(222)
	if err != nil {
		return nil, err
	}

	// DotReader handles the NNTP "dot-stuffing" (terminating the stream with .\r\n)
	return r.conn.DotReader(), nil
}

func (r *Repository) Close() error {
	if r.conn != nil {
		// Send the NNTP QUIT command so the server can release
		// the connection slot immediately.
		r.conn.Cmd("QUIT")
		return r.conn.Close()
	}
	return nil
}
