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
	if header.PartEnd != 740000 {
		t.Fatalf("expected part end 740000, got %d", header.PartEnd)
	}
}

func TestReadYencHeaderParsesNonFirstPartRange(t *testing.T) {
	raw := strings.NewReader("=ybegin part=50 total=62 line=128 size=44040192 name=Wbostp9Yf138Oybk1yc93o.part02.rar\n=ypart begin=35123201 end=35840000\n")

	header, err := ReadYencHeader(raw)
	if err != nil {
		t.Fatalf("read yenc header: %v", err)
	}
	if header.PartNumber != 50 || header.TotalParts != 62 {
		t.Fatalf("expected part 50/62, got %d/%d", header.PartNumber, header.TotalParts)
	}
	if header.PartOffset != 35123200 || header.PartEnd != 35840000 {
		t.Fatalf("expected part range 35123200-35840000, got %d-%d", header.PartOffset, header.PartEnd)
	}
}
