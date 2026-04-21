package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
)

type articleRef struct {
	messageID     string
	articleNumber int64
}

func main() {
	var (
		configPath   string
		serverID     string
		group        string
		messageID    string
		articleNum   int64
		bodyBytes    int64
		articleBytes int64
		headLines    int
	)

	flag.StringVar(&configPath, "config", "config.yaml", "config file path")
	flag.StringVar(&serverID, "server", "", "server id from config.yaml (defaults to first server)")
	flag.StringVar(&group, "group", "", "newsgroup to select before article-number operations")
	flag.StringVar(&messageID, "message-id", "", "message-id to inspect")
	flag.Int64Var(&articleNum, "article-number", 0, "article number to inspect within --group")
	flag.Int64Var(&bodyBytes, "body-bytes", 4096, "max BODY bytes to print")
	flag.Int64Var(&articleBytes, "article-bytes", 8192, "max ARTICLE bytes to print")
	flag.IntVar(&headLines, "head-lines", 200, "max HEAD lines to print")
	flag.Parse()

	ref, err := normalizeArticleRef(messageID, articleNum)
	fatalIf(err)

	cfg, err := config.Load(configPath)
	fatalIf(err)

	server, err := chooseServer(cfg, serverID)
	fatalIf(err)

	conn, err := dialServer(server)
	fatalIf(err)
	defer conn.Close()

	fmt.Printf("server: %s (%s:%d tls=%t)\n", server.ID, server.Host, server.Port, server.TLS)

	if strings.TrimSpace(group) != "" {
		stats, err := selectGroup(conn, group)
		fatalIf(err)
		fmt.Printf("group: %s count=%d low=%d high=%d\n", stats.group, stats.count, stats.low, stats.high)
		if ref.articleNumber > 0 {
			if err := printStat(conn, ref); err != nil {
				fmt.Printf("stat: %v\n", err)
			}
			if err := printXOver(conn, ref.articleNumber); err != nil {
				fmt.Printf("xover: %v\n", err)
			}
		}
	}

	if err := printHead(conn, ref, headLines); err != nil {
		fmt.Printf("head: %v\n", err)
	}
	if err := printBody(conn, ref, bodyBytes); err != nil {
		fmt.Printf("body: %v\n", err)
	}
	if err := printArticle(conn, ref, articleBytes); err != nil {
		fmt.Printf("article: %v\n", err)
	}
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func normalizeArticleRef(messageID string, articleNumber int64) (articleRef, error) {
	ref := articleRef{
		messageID:     strings.TrimSpace(messageID),
		articleNumber: articleNumber,
	}
	if ref.messageID == "" && ref.articleNumber <= 0 {
		return articleRef{}, fmt.Errorf("either --message-id or --article-number must be provided")
	}
	if ref.messageID != "" && !strings.HasPrefix(ref.messageID, "<") {
		ref.messageID = "<" + ref.messageID + ">"
	}
	return ref, nil
}

func chooseServer(cfg *config.Config, serverID string) (config.ServerConfig, error) {
	if cfg == nil {
		return config.ServerConfig{}, fmt.Errorf("config is required")
	}
	if len(cfg.Servers) == 0 {
		return config.ServerConfig{}, fmt.Errorf("no servers configured")
	}
	if strings.TrimSpace(serverID) == "" {
		return cfg.Servers[0], nil
	}
	for _, server := range cfg.Servers {
		if server.ID == serverID {
			return server, nil
		}
	}
	return config.ServerConfig{}, fmt.Errorf("server %q not found in config", serverID)
}

func dialServer(server config.ServerConfig) (*textproto.Conn, error) {
	addr := net.JoinHostPort(server.Host, strconv.Itoa(server.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	var (
		netConn net.Conn
		err     error
	)
	if server.TLS {
		netConn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: server.Host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		netConn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	conn := textproto.NewConn(netConn)
	code, msg, err := conn.ReadCodeLine(200)
	if tpErr, ok := err.(*textproto.Error); ok && tpErr.Code == 201 {
		err = nil
		code = tpErr.Code
		msg = tpErr.Msg
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("nntp greeting failed (code %d): %s", code, msg)
	}

	if server.Username != "" {
		if _, err := conn.Cmd("AUTHINFO USER %s", server.Username); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth user: %w", err)
		}
		if _, _, err := conn.ReadCodeLine(381); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth user rejected: %w", err)
		}
		if _, err := conn.Cmd("AUTHINFO PASS %s", server.Password); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth pass: %w", err)
		}
		if _, _, err := conn.ReadCodeLine(281); err != nil {
			conn.Close()
			return nil, fmt.Errorf("auth pass rejected: %w", err)
		}
	}

	return conn, nil
}

type groupStats struct {
	group string
	count int64
	low   int64
	high  int64
}

func selectGroup(conn *textproto.Conn, group string) (groupStats, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return groupStats{}, fmt.Errorf("group is required")
	}
	if _, err := conn.Cmd("GROUP %s", group); err != nil {
		return groupStats{}, err
	}
	code, msg, err := conn.ReadCodeLine(211)
	if err != nil {
		return groupStats{}, fmt.Errorf("GROUP %s failed (code %d): %s", group, code, msg)
	}
	parts := strings.Fields(msg)
	if len(parts) < 4 {
		return groupStats{}, fmt.Errorf("unexpected GROUP response: %q", msg)
	}
	count, _ := strconv.ParseInt(parts[0], 10, 64)
	low, _ := strconv.ParseInt(parts[1], 10, 64)
	high, _ := strconv.ParseInt(parts[2], 10, 64)
	return groupStats{group: group, count: count, low: low, high: high}, nil
}

func printStat(conn *textproto.Conn, ref articleRef) error {
	label := formatArticleRef(ref)
	if _, err := conn.Cmd("STAT %s", label); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(223)
	if err != nil {
		return fmt.Errorf("STAT failed (code %d): %s", code, msg)
	}
	fmt.Printf("\n== STAT ==\n%s\n", msg)
	return nil
}

func printXOver(conn *textproto.Conn, articleNumber int64) error {
	if articleNumber <= 0 {
		return nil
	}
	if _, err := conn.Cmd("XOVER %d-%d", articleNumber, articleNumber); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(224)
	if err != nil {
		return fmt.Errorf("XOVER failed (code %d): %s", code, msg)
	}
	lines, err := readDotLines(conn)
	if err != nil {
		return err
	}
	fmt.Printf("\n== XOVER ==\n")
	if len(lines) == 0 {
		fmt.Println("(no rows)")
		return nil
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

func printHead(conn *textproto.Conn, ref articleRef, maxLines int) error {
	if _, err := conn.Cmd("HEAD %s", formatArticleRef(ref)); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(221)
	if err != nil {
		return fmt.Errorf("HEAD failed (code %d): %s", code, msg)
	}
	lines, err := readDotLines(conn)
	if err != nil {
		return err
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	fmt.Printf("\n== HEAD ==\n")
	for _, line := range lines {
		fmt.Println(line)
	}
	return nil
}

func printBody(conn *textproto.Conn, ref articleRef, maxBytes int64) error {
	if _, err := conn.Cmd("BODY %s", formatArticleRef(ref)); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(222)
	if err != nil {
		return fmt.Errorf("BODY failed (code %d): %s", code, msg)
	}
	text, truncated, err := readDotTextLimited(conn, maxBytes)
	if err != nil {
		return err
	}
	fmt.Printf("\n== BODY ==\n%s", text)
	if truncated {
		fmt.Printf("\n... [truncated after %d bytes]\n", maxBytes)
	}
	return nil
}

func printArticle(conn *textproto.Conn, ref articleRef, maxBytes int64) error {
	if _, err := conn.Cmd("ARTICLE %s", formatArticleRef(ref)); err != nil {
		return err
	}
	code, msg, err := conn.ReadCodeLine(220)
	if err != nil {
		return fmt.Errorf("ARTICLE failed (code %d): %s", code, msg)
	}
	text, truncated, err := readDotTextLimited(conn, maxBytes)
	if err != nil {
		return err
	}
	fmt.Printf("\n== ARTICLE ==\n%s", text)
	if truncated {
		fmt.Printf("\n... [truncated after %d bytes]\n", maxBytes)
	}
	return nil
}

func readDotLines(conn *textproto.Conn) ([]string, error) {
	reader := bufio.NewScanner(conn.DotReader())
	reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := []string{}
	for reader.Scan() {
		lines = append(lines, reader.Text())
	}
	if err := reader.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func readDotTextLimited(conn *textproto.Conn, maxBytes int64) (string, bool, error) {
	data, err := io.ReadAll(conn.DotReader())
	if err != nil {
		return "", false, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return string(data[:maxBytes]), true, nil
	}
	return string(data), false, nil
}

func formatArticleRef(ref articleRef) string {
	if ref.messageID != "" {
		return ref.messageID
	}
	return strconv.FormatInt(ref.articleNumber, 10)
}
