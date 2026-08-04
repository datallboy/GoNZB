// Command nntpfixture is a deterministic loopback-only server for GoNZBNet E2E tests.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	fixtureGroup     = "alt.binaries.test"
	fixtureMessageID = "<gonzbnet-e2e-1@example.invalid>"
)

type fixtureArticle struct {
	number     int
	messageID  string
	subject    string
	fileName   string
	bytes      int
	part       int
	totalParts int
}

var fixtureArticles = []fixtureArticle{
	{1, fixtureMessageID, `GoNZBNet.E2E.Indexer.Release.2026.1080p [1/3] - "GoNZBNet.E2E.Indexer.Release.2026.1080p.mkv" yEnc (1/2)`, "GoNZBNet.E2E.Indexer.Release.2026.1080p.mkv", 367001600, 1, 2},
	{2, "<gonzbnet-e2e-2@example.invalid>", `GoNZBNet.E2E.Indexer.Release.2026.1080p [1/3] - "GoNZBNet.E2E.Indexer.Release.2026.1080p.mkv" yEnc (2/2)`, "GoNZBNet.E2E.Indexer.Release.2026.1080p.mkv", 367001600, 2, 2},
	{3, "<gonzbnet-e2e-3@example.invalid>", `GoNZBNet.E2E.Indexer.Release.2026.1080p [2/3] - "GoNZBNet.E2E.Indexer.Release.2026.1080p.nfo" yEnc (1/1)`, "GoNZBNet.E2E.Indexer.Release.2026.1080p.nfo", 4096, 1, 1},
	{4, "<gonzbnet-e2e-4@example.invalid>", `GoNZBNet.E2E.Indexer.Release.2026.1080p [3/3] - "GoNZBNet.E2E.Indexer.Release.2026.1080p.par2" yEnc (1/1)`, "GoNZBNet.E2E.Indexer.Release.2026.1080p.par2", 8192, 1, 1},
}

func main() {
	address := flag.String("listen", "127.0.0.1:11119", "listen address")
	flag.Parse()
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go serve(connection)
	}
}

func serve(connection net.Conn) {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	writeLine(writer, "200 GoNZBNet deterministic NNTP fixture ready")
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
		case "GROUP":
			if len(parts) == 2 && parts[1] == fixtureGroup {
				writeLine(writer, fmt.Sprintf("211 %d 1 %d %s", len(fixtureArticles), len(fixtureArticles), fixtureGroup))
			} else {
				writeLine(writer, "411 no such news group")
			}
		case "XOVER", "OVER":
			writeLine(writer, "224 Overview information follows")
			date := time.Now().UTC().Format(time.RFC1123Z)
			for _, article := range fixtureArticles {
				writeLine(writer, fmt.Sprintf("%d\t%s\te2e@example.invalid\t%s\t%s\t\t%d\t32\tXref: fixture %s:%d", article.number, article.subject, date, article.messageID, article.bytes, fixtureGroup, article.number))
			}
			writeLine(writer, ".")
		case "BODY":
			if article, ok := findArticle(parts); ok {
				writeLine(writer, fmt.Sprintf("222 %d %s body follows", article.number, article.messageID))
				writeLine(writer, fmt.Sprintf("=ybegin part=%d total=%d line=128 size=%d name=%s", article.part, article.totalParts, article.bytes*article.totalParts, article.fileName))
				if article.totalParts > 1 {
					begin := (article.part-1)*article.bytes + 1
					writeLine(writer, fmt.Sprintf("=ypart begin=%d end=%d", begin, begin+article.bytes-1))
				}
				writeLine(writer, "fixture-payload")
				writeLine(writer, fmt.Sprintf("=yend size=%d part=%d", article.bytes, article.part))
				writeLine(writer, ".")
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

func findArticle(parts []string) (fixtureArticle, bool) {
	if len(parts) != 2 {
		return fixtureArticle{}, false
	}
	messageID := normalizeMessageID(parts[1])
	for _, article := range fixtureArticles {
		if article.messageID == messageID {
			return article, true
		}
	}
	return fixtureArticle{}, false
}

func normalizeMessageID(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "<") {
		value = "<" + value
	}
	if !strings.HasSuffix(value, ">") {
		value += ">"
	}
	return value
}

func writeLine(writer *bufio.Writer, value string) {
	_, _ = writer.WriteString(value + "\r\n")
	_ = writer.Flush()
}
