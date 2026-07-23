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
				writeLine(writer, "211 1 1 1 "+fixtureGroup)
			} else {
				writeLine(writer, "411 no such news group")
			}
		case "XOVER", "OVER":
			writeLine(writer, "224 Overview information follows")
			date := time.Now().UTC().Format(time.RFC1123Z)
			writeLine(writer, fmt.Sprintf("1\tGoNZBNet.E2E.Release.1080p.mkv yEnc (1/1)\te2e@example.invalid\t%s\t%s\t\t2048\t32\tXref: fixture %s:1", date, fixtureMessageID, fixtureGroup))
			writeLine(writer, ".")
		case "BODY":
			if len(parts) == 2 && normalizeMessageID(parts[1]) == fixtureMessageID {
				writeLine(writer, "222 1 "+fixtureMessageID+" body follows")
				writeLine(writer, "=ybegin line=128 size=16 name=GoNZBNet.E2E.Release.1080p.mkv")
				writeLine(writer, "fixture-payload")
				writeLine(writer, "=yend size=16")
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
