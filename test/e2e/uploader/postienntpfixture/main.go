// Command postienntpfixture is a loopback-only NNTP posting server used by the
// optional Postie uploader conformance test. It accepts synthetic POSTs,
// records bounded metadata about them, and answers STAT for accepted articles.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxArticleBytes = 8 << 20

type capturedArticle struct {
	MessageID     string   `json:"message_id"`
	Subject       string   `json:"subject"`
	From          string   `json:"from"`
	Newsgroups    []string `json:"newsgroups"`
	ArticleSHA    string   `json:"article_sha256"`
	ArticleBytes  int      `json:"article_bytes"`
	BodySHA       string   `json:"body_sha256"`
	BodyBytes     int      `json:"body_bytes"`
	YEncName      string   `json:"yenc_name,omitempty"`
	YEncPart      int      `json:"yenc_part,omitempty"`
	YEncTotal     int      `json:"yenc_total,omitempty"`
	YEncPartBytes int      `json:"yenc_part_bytes,omitempty"`
}

type fixture struct {
	capturePath string
	mu          sync.RWMutex
	articles    map[string]capturedArticle
}

func main() {
	address := flag.String("listen", "127.0.0.1:11120", "loopback listen address")
	capturePath := flag.String("capture", "", "JSONL capture output")
	readyPath := flag.String("ready-file", "", "file written after the listener is ready")
	flag.Parse()

	listener, err := net.Listen("tcp", *address)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	server := &fixture{capturePath: *capturePath, articles: make(map[string]capturedArticle)}
	if *readyPath != "" {
		if err := os.WriteFile(*readyPath, []byte(listener.Addr().String()+"\n"), 0o600); err != nil {
			panic(err)
		}
	}

	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go server.serve(connection)
	}
}

func (f *fixture) serve(connection net.Conn) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	writeLine(writer, "200 GoNZB Postie conformance fixture ready")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 0 {
			continue
		}

		switch strings.ToUpper(parts[0]) {
		case "DATE":
			writeLine(writer, "111 "+time.Now().UTC().Format("20060102150405"))
		case "CAPABILITIES":
			writeLine(writer, "101 Capability list follows")
			writeLine(writer, "VERSION 2")
			writeLine(writer, "READER")
			writeLine(writer, "POST")
			writeLine(writer, ".")
		case "HELP":
			writeLine(writer, "100 Help text follows")
			writeLine(writer, "DATE POST STAT QUIT")
			writeLine(writer, ".")
		case "AUTHINFO":
			if len(parts) >= 2 && strings.EqualFold(parts[1], "USER") {
				writeLine(writer, "381 password required")
			} else if len(parts) >= 2 && strings.EqualFold(parts[1], "PASS") {
				writeLine(writer, "281 authentication accepted")
			} else {
				writeLine(writer, "501 invalid AUTHINFO command")
			}
		case "POST":
			writeLine(writer, "340 send article")
			article, err := readArticle(reader)
			if err != nil {
				writeLine(writer, "441 posting failed")
				return
			}
			if err := f.record(article); err != nil {
				writeLine(writer, "441 capture failed")
				return
			}
			writeLine(writer, "240 article received")
		case "STAT":
			if len(parts) != 2 {
				writeLine(writer, "501 message-id required")
				continue
			}
			messageID := normalizeMessageID(parts[1])
			if f.has(messageID) {
				writeLine(writer, "223 1 "+messageID)
			} else {
				writeLine(writer, "430 no such article")
			}
		case "QUIT":
			writeLine(writer, "205 closing connection")
			return
		default:
			writeLine(writer, "500 unsupported command")
		}
	}
}

func readArticle(reader *bufio.Reader) (capturedArticle, error) {
	var raw []byte
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return capturedArticle{}, err
		}
		if string(line) == ".\r\n" || string(line) == ".\n" {
			break
		}
		if len(line) >= 2 && line[0] == '.' && line[1] == '.' {
			line = line[1:]
		}
		if len(raw)+len(line) > maxArticleBytes {
			return capturedArticle{}, fmt.Errorf("article exceeds %d bytes", maxArticleBytes)
		}
		raw = append(raw, line...)
	}

	headerBytes, body, ok := splitArticle(raw)
	if !ok {
		return capturedArticle{}, errors.New("article has no header/body separator")
	}
	headers, err := parseHeaders(headerBytes)
	if err != nil {
		return capturedArticle{}, err
	}
	messageID := normalizeMessageID(headers["message-id"])
	if messageID == "<>" {
		return capturedArticle{}, errors.New("article has no Message-ID")
	}

	articleHash := sha256.Sum256(raw)
	bodyHash := sha256.Sum256(body)
	yencName, yencPart, yencTotal, yencPartBytes := parseYEnc(body)
	groups := splitCSV(headers["newsgroups"])
	if len(groups) == 0 {
		return capturedArticle{}, errors.New("article has no Newsgroups header")
	}

	return capturedArticle{
		MessageID: messageID, Subject: headers["subject"], From: headers["from"], Newsgroups: groups,
		ArticleSHA: hex.EncodeToString(articleHash[:]), ArticleBytes: len(raw), BodySHA: hex.EncodeToString(bodyHash[:]),
		BodyBytes: len(body), YEncName: yencName, YEncPart: yencPart, YEncTotal: yencTotal, YEncPartBytes: yencPartBytes,
	}, nil
}

func splitArticle(raw []byte) ([]byte, []byte, bool) {
	for _, separator := range []string{"\r\n\r\n", "\n\n"} {
		if index := strings.Index(string(raw), separator); index >= 0 {
			return raw[:index], raw[index+len(separator):], true
		}
	}
	return nil, nil, false
}

func parseHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var current string
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && current != "" {
			headers[current] += " " + strings.TrimSpace(line)
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid article header %q", line)
		}
		current = strings.ToLower(strings.TrimSpace(name))
		headers[current] = strings.TrimSpace(value)
	}
	return headers, scanner.Err()
}

func parseYEnc(body []byte) (string, int, int, int) {
	name := ""
	part := 0
	total := 0
	partBegin := 0
	partEnd := 0
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if !strings.HasPrefix(line, "=ybegin ") && !strings.HasPrefix(line, "=ypart ") {
			continue
		}
		fields := strings.Fields(line)
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "name":
				name = value
			case "part":
				part, _ = strconv.Atoi(value)
			case "total":
				total, _ = strconv.Atoi(value)
			case "begin":
				partBegin, _ = strconv.Atoi(value)
			case "end":
				partEnd, _ = strconv.Atoi(value)
			}
		}
	}
	partBytes := 0
	if partBegin > 0 && partEnd >= partBegin {
		partBytes = partEnd - partBegin + 1
	}
	return name, part, total, partBytes
}

func (f *fixture) record(article capturedArticle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.articles[article.MessageID]; exists {
		return nil
	}
	f.articles[article.MessageID] = article
	if f.capturePath == "" {
		return nil
	}
	file, err := os.OpenFile(f.capturePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(article)
}

func (f *fixture) has(messageID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.articles[messageID]
	return ok
}

func normalizeMessageID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "<")
	value = strings.TrimSuffix(value, ">")
	return "<" + value + ">"
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func writeLine(writer *bufio.Writer, value string) {
	_, _ = io.WriteString(writer, value+"\r\n")
	_ = writer.Flush()
}
