package nzb

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
	PartNumber  int
	TotalParts  int
	PartOffset  int64
	PartEnd     int64
	FileSize    int64
	FileName    string
}

type YencHeader struct {
	PartNumber int
	TotalParts int
	PartOffset int64
	PartEnd    int64
	FileSize   int64
	FileName   string
}

func NewYencDecoder(r io.Reader) *YencDecoder {
	return &YencDecoder{
		scanner: bufio.NewReader(r),
		hash:    crc32.NewIEEE(), // yEnc uses the standard IEEE polynomial
	}
}

func ReadYencHeader(r io.Reader) (YencHeader, error) {
	reader := bufio.NewReader(r)
	var header YencHeader

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return header, fmt.Errorf("searching for yenc header: %w", err)
		}

		if strings.HasPrefix(line, "=ybegin") {
			parseYbeginLine(line, &header)
			return header, readPotentialPartHeader(reader, &header)
		}
	}
}

func (d *YencDecoder) DiscardHeader() error {
	for {
		line, err := d.scanner.ReadString('\n')
		if err != nil {
			return fmt.Errorf("searching for yenc header: %w", err)
		}

		if strings.HasPrefix(line, "=ybegin") {
			d.parseYbegin(line)
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
			d.hash.Write(p[:n])
			return n, err
		}

		// Handle yEnc Escape character
		if b == '=' && !d.escaped {
			// Peek ahead to see if this is actually the end of the file
			peek, err := d.scanner.Peek(4)
			if err == nil && string(peek) == "yend" {
				d.reachedEnd = true
				d.parseFooter() // Extract CRC from the footer

				d.hash.Write(p[:n])
				return n, io.EOF
			}

			d.escaped = true
			continue
		}

		if (b == '\r' || b == '\n') && !d.escaped {
			// yEnc ignores critical characters (newlines) unless they are escaped.
			// If escaped is true, it shouldn't be a newline, but we reset anyway.
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
		n++
	}

	// Update hash once per Read call
	d.hash.Write(p[:n])
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
	// A CRC of 00000000 in yEnc often indicates obfuscated data.
	// Trust the data; PAR2 will verify it later.
	if d.expectedCRC == 0 {
		return nil
	}

	actual := d.hash.Sum32()
	if actual != d.expectedCRC {
		return fmt.Errorf("checksum mismatch: expected %08X, got %08X", d.expectedCRC, actual)
	}
	return nil
}

func (d *YencDecoder) parseYbegin(line string) {
	header := YencHeader{PartNumber: d.PartNumber, TotalParts: d.TotalParts, PartOffset: d.PartOffset, PartEnd: d.PartEnd, FileSize: d.FileSize, FileName: d.FileName}
	parseYbeginLine(line, &header)
	d.PartNumber = header.PartNumber
	d.TotalParts = header.TotalParts
	d.PartOffset = header.PartOffset
	d.PartEnd = header.PartEnd
	d.FileSize = header.FileSize
	d.FileName = header.FileName
}

func parseYbeginLine(line string, header *YencHeader) {
	parts := strings.Fields(line)
	for _, part := range parts {
		if strings.HasPrefix(part, "part=") {
			val := strings.TrimPrefix(part, "part=")
			if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
				header.PartNumber = parsed
			}
			continue
		}
		if strings.HasPrefix(part, "total=") {
			val := strings.TrimPrefix(part, "total=")
			if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
				header.TotalParts = parsed
			}
			continue
		}
		if strings.HasPrefix(part, "size=") {
			val := strings.TrimPrefix(part, "size=")
			size, err := strconv.ParseInt(val, 10, 64)
			if err == nil {
				header.FileSize = size
			}
			continue
		}
		if strings.HasPrefix(part, "name=") {
			header.FileName = strings.TrimSpace(strings.TrimPrefix(part, "name="))
		}
	}
}

func (d *YencDecoder) handlePotentialPartHeader() error {
	header := YencHeader{PartNumber: d.PartNumber, TotalParts: d.TotalParts, PartOffset: d.PartOffset, PartEnd: d.PartEnd, FileSize: d.FileSize, FileName: d.FileName}
	if err := readPotentialPartHeader(d.scanner, &header); err != nil {
		return err
	}
	d.PartNumber = header.PartNumber
	d.TotalParts = header.TotalParts
	d.PartOffset = header.PartOffset
	d.PartEnd = header.PartEnd
	d.FileSize = header.FileSize
	d.FileName = header.FileName
	return nil
}

func readPotentialPartHeader(reader *bufio.Reader, header *YencHeader) error {
	// Peek at the next few bytes to see if =ypart follows
	// We use Peek so we don't consume the data if it's actually binary
	peek, err := reader.Peek(6)
	if err != nil {
		return nil
	}

	peekStr := string(peek)

	if strings.Contains(peekStr, "=ypart") {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		// Extract the "begin" offset
		// Example line: =ypart begin=1 end=734000
		parts := strings.Fields(line)
		for _, part := range parts {
			if strings.HasPrefix(part, "begin=") {
				val := strings.TrimPrefix(part, "begin=")
				// yEnc offsets are 1-based, we convert to 0-based for disk I/O
				offset, err := strconv.ParseInt(val, 10, 64)
				if err == nil {
					header.PartOffset = offset - 1
				}
			}
			if strings.HasPrefix(part, "end=") {
				val := strings.TrimPrefix(part, "end=")
				end, err := strconv.ParseInt(val, 10, 64)
				if err == nil && end > 0 {
					header.PartEnd = end
				}
			}
		}

	}
	return nil
}
