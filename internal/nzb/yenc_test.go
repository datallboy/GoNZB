package nzb

import (
	"strings"
	"testing"
)

func TestReadYencHeaderParsesFileSizeAndPartOffset(t *testing.T) {
	raw := strings.NewReader("garbage line\n=ybegin part=1 total=220 line=128 size=162347442 name=example.7z.055\n=ypart begin=1 end=740000\n")

	header, err := ReadYencHeader(raw)
	if err != nil {
		t.Fatalf("read yenc header: %v", err)
	}
	if header.FileSize != 162347442 {
		t.Fatalf("expected file size 162347442, got %d", header.FileSize)
	}
	if header.PartOffset != 0 {
		t.Fatalf("expected part offset 0, got %d", header.PartOffset)
	}
}
