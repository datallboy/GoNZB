package decoding

import (
	"bufio"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
)

var ErrHeaderNotFound = errors.New("yenc header not found")

type YencDecoder struct {
	scanner     *bufio.Reader
	reachedEnd  bool
	escaped     bool // State: was the previous byte '='?
	hash        hash.Hash32
	expectedCRC uint32
}

func NewYencDecoder(r io.Reader) *YencDecoder {
	return &YencDecoder{
		scanner: bufio.NewReader(r),
		hash:    crc32.NewIEEE(), // yEnc uses the standard IEEE polynomial
	}
}

func (d *YencDecoder) DiscardHeader() error {
	for {
		line, err := d.scanner.ReadString('\n')
		if err != nil {
			return fmt.Errorf("searching for yenc header: %w", err)
		}

		if strings.HasPrefix(line, "=ybegin") {
			return d.handlePotentialPartHeader()
		}
	}
}

func (d *YencDecoder) Read(p []byte) (n int, err error) {
	if d.reachedEnd {
		return 0, io.EOF
	}

	for n < len(p) {
		b, err := d.scanner.ReadByte()
		if err != nil {
			return n, err
		}

		// Handle yEnc Escape character
		if b == '=' && !d.escaped {
			// Peek ahead to see if this is actually the end of the file
			peek, _ := d.scanner.Peek(4)
			if len(peek) >= 4 && string(peek) == "yend" {
				d.reachedEnd = true
				d.parseFooter() // Extract CRC from the footer
				return n, io.EOF
			}

			d.escaped = true
			continue
		}

		if b == '\r' || b == '\n' {
			// yEnc ignores critical characters (newlines) unless they are escaped.
			// If escaped is true, it shouldn't be a newline, but we reset anyway.
			d.escaped = false
			continue
		}

		// Decode the byte
		var decoded byte
		if d.escaped {
			decoded = b - 64 - 42
			d.escaped = false
		} else {
			decoded = b - 42
		}

		p[n] = decoded
		d.hash.Write(p[n : n+1])
		n++
	}

	return n, nil
}

func (d *YencDecoder) parseFooter() {
	line, _ := d.scanner.ReadString('\n')
	// Typical footer: =yend size=12345 pcrc32=ABC12345
	parts := strings.Fields(line)
	for _, part := range parts {
		if strings.HasPrefix(part, "pcrc32=") {
			val := strings.TrimPrefix(part, "pcrc32=")
			crc, err := strconv.ParseUint(val, 16, 32)
			if err == nil {
				d.expectedCRC = uint32(crc)
				return // Found the part CRC, we can stop
			}
		}
		// Fallback to crc32 if pcrc32 isn't there
		if strings.HasPrefix(part, "crc32=") {
			val := strings.TrimPrefix(part, "crc32=")
			crc, err := strconv.ParseUint(val, 16, 32)
			if err == nil {
				d.expectedCRC = uint32(crc)
			}
		}
	}
}

func (d *YencDecoder) Verify() error {
	actual := d.hash.Sum32()
	if actual != d.expectedCRC {
		return fmt.Errorf("checksum mismatch: expected %08X, got %08X", d.expectedCRC, actual)
	}
	return nil
}

func (d *YencDecoder) handlePotentialPartHeader() error {
	// Peek at the next few bytes to see if =ypart follows
	// We use Peek so we don't consume the data if it's actually binary
	peek, _ := d.scanner.Peek(100)
	peekStr := string(peek)

	if strings.Contains(peekStr, "=ypart") {
		_, err := d.scanner.ReadString('\n')
		return err
	}
	return nil
}
