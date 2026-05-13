package par2

import (
	"encoding/binary"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	par2PacketHeaderSize = 64
	par2FileDescBodySize = 56
	maxPAR2TargetSize    = uint64(1 << 50) // 1 PiB, far above useful Usenet payloads.
)

var (
	par2PacketMagic    = []byte("PAR2\x00PKT")
	par2FileDescPacket = "PAR 2.0\x00FileDesc"
)

type targetFile struct {
	Name string
	Size uint64
}

func parseTargetFiles(data []byte) []targetFile {
	out := make([]targetFile, 0)
	seen := map[string]struct{}{}
	for offset := 0; offset+par2PacketHeaderSize <= len(data); {
		if string(data[offset:offset+8]) != string(par2PacketMagic) {
			offset++
			continue
		}
		packetLen := binary.LittleEndian.Uint64(data[offset+8 : offset+16])
		if packetLen < par2PacketHeaderSize || packetLen > uint64(len(data)-offset) {
			break
		}
		packet := data[offset : offset+int(packetLen)]
		packetType := strings.TrimRight(string(packet[48:64]), "\x00")
		if packetType == par2FileDescPacket {
			if target, ok := parseFileDescPacket(packet); ok {
				key := strings.ToLower(target.Name)
				if _, exists := seen[key]; !exists {
					seen[key] = struct{}{}
					out = append(out, target)
				}
			}
		}
		offset += int(packetLen)
	}
	return out
}

func parseFileDescPacket(packet []byte) (targetFile, bool) {
	if len(packet) < par2PacketHeaderSize+par2FileDescBodySize {
		return targetFile{}, false
	}
	body := packet[par2PacketHeaderSize:]
	size := binary.LittleEndian.Uint64(body[48:56])
	if size == 0 || size > uint64(math.MaxInt64) || size > maxPAR2TargetSize {
		return targetFile{}, false
	}
	nameBytes := body[56:]
	if idx := indexByte(nameBytes, 0); idx >= 0 {
		nameBytes = nameBytes[:idx]
	}
	name := strings.TrimSpace(string(nameBytes))
	if !validTargetName(name) {
		return targetFile{}, false
	}
	return targetFile{Name: name, Size: size}, true
}

func validTargetName(name string) bool {
	if name == "" || len(name) > 512 || !utf8.ValidString(name) {
		return false
	}
	hasGraphic := false
	for _, r := range name {
		if r == '/' || r == '\\' || r == '.' || r == '_' || r == '-' || r == ' ' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasGraphic = true
			continue
		}
		if unicode.IsControl(r) || unicode.IsMark(r) || unicode.IsSymbol(r) {
			return false
		}
		return false
	}
	return hasGraphic
}

func indexByte(in []byte, target byte) int {
	for i, b := range in {
		if b == target {
			return i
		}
	}
	return -1
}
