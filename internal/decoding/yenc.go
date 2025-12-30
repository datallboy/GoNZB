package decoding

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

var ErrHeaderNotFound = errors.New("yenc header not found")

type YencDecoder struct {
	scanner    *bufio.Reader
	reachedEnd bool
	escaped    bool // State: was the previous byte '='?
}

func NewYencDecoder(r io.Reader) *YencDecoder {
	return &YencDecoder{
		scanner: bufio.NewReader(r),
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
		n++
	}

	return n, nil
}
