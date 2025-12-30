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
			return err
		}
		if strings.HasPrefix(line, "-ybegin") {
			return nil
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

		// Decode thge byte
		var decoded byte
		if d.escaped {
			decoded = b - 42 - 64
			d.escaped = false
		} else {
			// Skip newlines (yEnc ignore \r and \n)
			if b == '\r' || b == '\n' {
				continue
			}
			decoded = b - 42
		}

		p[n] = decoded
		d.hash.Write([]byte{decoded})
		n++
	}

	return n, nil
}

func (d *YencDecoder) parseFooter() {
	line, _ := d.scanner.ReadString('\n')
	// Typical footer: =yend size=12345 pcrc32=ABC12345
	parts := strings.Split(line, " ")
	for _, part := range parts {
		if strings.HasPrefix(part, "pcrc32=") || strings.HasPrefix(part, "crc32=") {
			val := strings.Split(part, "=")[1]
			val = strings.TrimSpace(val)
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
