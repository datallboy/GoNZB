package nzb

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

const canonicalDoctype = `<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">`

// SanitizeBytes validates an NZB and emits the canonical private form used by
// worker uploads and GoNZBNet manifest reconstruction. Descriptive head
// metadata is intentionally discarded; only the extraction password remains.
func SanitizeBytes(data []byte, limits Limits) ([]byte, string, error) {
	doc, err := ValidateBytes(data, limits)
	if err != nil {
		return nil, "", err
	}
	payload, err := CanonicalBytes(doc.Model, doc.Facts.Password)
	if err != nil {
		return nil, "", err
	}
	return payload, doc.Facts.Password, nil
}

// CanonicalBytes serializes a validated NZB model deterministically using the
// Pesto 0.8.6 layout. Message IDs are stored internally with angle brackets
// and written to NZB segment bodies without them, as required by that format.
func CanonicalBytes(model *Model, password string) ([]byte, error) {
	if model == nil || len(model.Files) == 0 {
		return nil, fmt.Errorf("nzb model requires at least one file")
	}
	var out strings.Builder
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	out.WriteString(canonicalDoctype)
	out.WriteByte('\n')
	out.WriteString("<nzb xmlns=\"http://www.newzbin.com/DTD/2003/nzb\">\n")
	out.WriteString("  <head>\n")
	if password != "" {
		fmt.Fprintf(&out, "    <meta type=\"password\">%s</meta>\n", escapeCanonicalXML(password))
	}
	out.WriteString("  </head>\n")

	for fileIndex, file := range model.Files {
		subject := strings.TrimSpace(file.Subject)
		if subject == "" {
			return nil, fmt.Errorf("nzb file %d requires a subject", fileIndex+1)
		}
		poster := strings.TrimSpace(file.Poster)
		if poster == "" {
			poster = "unknown"
		}
		fmt.Fprintf(&out, "  <file poster=\"%s\" date=\"%d\" subject=\"%s\">\n",
			escapeCanonicalXML(poster), file.Date, escapeCanonicalXML(subject))
		out.WriteString("    <groups>\n")
		for _, group := range canonicalGroups(file.Groups) {
			fmt.Fprintf(&out, "      <group>%s</group>\n", escapeCanonicalXML(group))
		}
		out.WriteString("    </groups>\n")
		out.WriteString("    <segments>\n")
		for _, segment := range SortedSegments(file) {
			messageID, err := NormalizeMessageID(segment.MessageID)
			if err != nil {
				return nil, fmt.Errorf("nzb file %d segment %d: %w", fileIndex+1, segment.Number, err)
			}
			messageID = strings.TrimSuffix(strings.TrimPrefix(messageID, "<"), ">")
			fmt.Fprintf(&out, "      <segment bytes=\"%d\" number=\"%d\">%s</segment>\n",
				segment.Bytes, segment.Number, escapeCanonicalXML(messageID))
		}
		out.WriteString("    </segments>\n")
		out.WriteString("  </file>\n")
	}
	out.WriteString("</nzb>\n")
	return []byte(out.String()), nil
}

func canonicalGroups(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func escapeCanonicalXML(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for _, char := range value {
		switch char {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '"':
			out.WriteString("&quot;")
		case '\'':
			out.WriteString("&apos;")
		default:
			if unicode.IsControl(char) && char != '\t' && char != '\n' && char != '\r' {
				continue
			}
			out.WriteRune(char)
		}
	}
	return out.String()
}
