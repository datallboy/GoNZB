package nzb

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html/charset"
)

const (
	DefaultMaxNZBBytes       int64 = 64 << 20
	DefaultMaxFiles                = 100_000
	DefaultMaxSegments             = 5_000_000
	DefaultMaxXMLDepth             = 32
	DefaultMaxMetadataLength       = 16 << 10
)

// Limits bounds the amount of work performed while validating an NZB.
type Limits struct {
	MaxBytes          int64
	MaxFiles          int
	MaxSegments       int
	MaxXMLDepth       int
	MaxMetadataLength int
}

func DefaultLimits() Limits {
	return Limits{
		MaxBytes:          DefaultMaxNZBBytes,
		MaxFiles:          DefaultMaxFiles,
		MaxSegments:       DefaultMaxSegments,
		MaxXMLDepth:       DefaultMaxXMLDepth,
		MaxMetadataLength: DefaultMaxMetadataLength,
	}
}

type FileFacts struct {
	Name         string    `json:"name"`
	Subject      string    `json:"subject"`
	Poster       string    `json:"poster"`
	PostedAt     time.Time `json:"posted_at"`
	Groups       []string  `json:"groups"`
	SizeBytes    int64     `json:"size_bytes"`
	SegmentCount int       `json:"segment_count"`
}

// Facts contains trustworthy values derived from the original NZB.
type Facts struct {
	SizeBytes    int64       `json:"size_bytes"`
	FileCount    int         `json:"file_count"`
	SegmentCount int         `json:"segment_count"`
	Groups       []string    `json:"groups"`
	Poster       string      `json:"poster"`
	PostedAt     time.Time   `json:"posted_at"`
	Title        string      `json:"title"`
	Password     string      `json:"-"`
	HasPAR2      bool        `json:"has_par2"`
	HasNFO       bool        `json:"has_nfo"`
	Files        []FileFacts `json:"files"`
}

type ValidatedDocument struct {
	Model *Model
	Facts Facts
}

// ValidateBytes parses and validates one complete NZB XML document. Message
// IDs are normalized in the returned model to their bracketed representation.
func ValidateBytes(data []byte, limits Limits) (*ValidatedDocument, error) {
	limits = normalizeLimits(limits)
	if len(data) == 0 {
		return nil, fmt.Errorf("nzb is empty")
	}
	if int64(len(data)) > limits.MaxBytes {
		return nil, fmt.Errorf("nzb exceeds %d byte limit", limits.MaxBytes)
	}
	if err := validateXMLShape(data, limits.MaxXMLDepth); err != nil {
		return nil, err
	}

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	var model Model
	if err := decoder.Decode(&model); err != nil {
		return nil, fmt.Errorf("decode nzb: %w", err)
	}
	if model.XMLName.Local != "nzb" {
		return nil, fmt.Errorf("root element must be nzb")
	}
	if len(model.Files) == 0 {
		return nil, fmt.Errorf("nzb must contain at least one file")
	}
	if len(model.Files) > limits.MaxFiles {
		return nil, fmt.Errorf("nzb exceeds %d file limit", limits.MaxFiles)
	}

	facts := Facts{
		FileCount: len(model.Files),
		Files:     make([]FileFacts, 0, len(model.Files)),
	}
	groupsSeen := make(map[string]struct{})

	for _, meta := range model.Meta {
		if len(meta.Type) > limits.MaxMetadataLength || len(meta.Content) > limits.MaxMetadataLength {
			return nil, fmt.Errorf("nzb metadata exceeds %d byte field limit", limits.MaxMetadataLength)
		}
		typeName := strings.ToLower(strings.TrimSpace(meta.Type))
		value := strings.TrimSpace(meta.Content)
		switch typeName {
		case "password":
			if facts.Password == "" {
				facts.Password = value
			}
		case "name", "title":
			if facts.Title == "" {
				facts.Title = value
			}
		}
	}

	for fileIndex := range model.Files {
		file := &model.Files[fileIndex]
		file.Subject = strings.TrimSpace(file.Subject)
		file.Poster = strings.TrimSpace(file.Poster)
		if len(file.Subject) > limits.MaxMetadataLength || len(file.Poster) > limits.MaxMetadataLength {
			return nil, fmt.Errorf("file %d subject or poster exceeds field limit", fileIndex+1)
		}
		if len(file.Segments) == 0 {
			return nil, fmt.Errorf("file %d must contain at least one segment", fileIndex+1)
		}
		if facts.SegmentCount > limits.MaxSegments-len(file.Segments) {
			return nil, fmt.Errorf("nzb exceeds %d segment limit", limits.MaxSegments)
		}

		fileFacts := FileFacts{
			Name:         SubjectFilename(file.Subject),
			Subject:      file.Subject,
			Poster:       file.Poster,
			SegmentCount: len(file.Segments),
		}
		if file.Date > 0 {
			fileFacts.PostedAt = time.Unix(file.Date, 0).UTC()
			if facts.PostedAt.IsZero() || fileFacts.PostedAt.After(facts.PostedAt) {
				facts.PostedAt = fileFacts.PostedAt
			}
		}
		if facts.Poster == "" && file.Poster != "" {
			facts.Poster = file.Poster
		}

		fileGroupsSeen := make(map[string]struct{})
		fileGroups := make([]string, 0, len(file.Groups))
		for _, rawGroup := range file.Groups {
			group := strings.TrimSpace(rawGroup)
			if group == "" {
				continue
			}
			if !validNewsgroup(group) {
				return nil, fmt.Errorf("file %d contains invalid newsgroup", fileIndex+1)
			}
			if _, exists := fileGroupsSeen[group]; exists {
				continue
			}
			fileGroupsSeen[group] = struct{}{}
			fileGroups = append(fileGroups, group)
			if _, exists := groupsSeen[group]; !exists {
				groupsSeen[group] = struct{}{}
				facts.Groups = append(facts.Groups, group)
			}
		}
		file.Groups = fileGroups
		fileFacts.Groups = append([]string(nil), fileGroups...)

		segmentNumbers := make(map[int]struct{}, len(file.Segments))
		for segmentIndex := range file.Segments {
			segment := &file.Segments[segmentIndex]
			if segment.Number <= 0 {
				return nil, fmt.Errorf("file %d segment %d has invalid number", fileIndex+1, segmentIndex+1)
			}
			if _, exists := segmentNumbers[segment.Number]; exists {
				return nil, fmt.Errorf("file %d contains duplicate segment number %d", fileIndex+1, segment.Number)
			}
			segmentNumbers[segment.Number] = struct{}{}
			if segment.Bytes <= 0 {
				return nil, fmt.Errorf("file %d segment %d has invalid byte count", fileIndex+1, segmentIndex+1)
			}
			normalizedID, err := NormalizeMessageID(segment.MessageID)
			if err != nil {
				return nil, fmt.Errorf("file %d segment %d: %w", fileIndex+1, segmentIndex+1, err)
			}
			segment.MessageID = normalizedID
			if facts.SizeBytes > math.MaxInt64-segment.Bytes {
				return nil, fmt.Errorf("nzb byte total overflows int64")
			}
			facts.SizeBytes += segment.Bytes
			fileFacts.SizeBytes += segment.Bytes
		}
		facts.SegmentCount += len(file.Segments)
		nameLower := strings.ToLower(fileFacts.Name + " " + file.Subject)
		facts.HasPAR2 = facts.HasPAR2 || strings.Contains(nameLower, ".par2")
		facts.HasNFO = facts.HasNFO || strings.Contains(nameLower, ".nfo")
		facts.Files = append(facts.Files, fileFacts)
	}

	return &ValidatedDocument{Model: &model, Facts: facts}, nil
}

func normalizeLimits(in Limits) Limits {
	defaults := DefaultLimits()
	if in.MaxBytes <= 0 {
		in.MaxBytes = defaults.MaxBytes
	}
	if in.MaxFiles <= 0 {
		in.MaxFiles = defaults.MaxFiles
	}
	if in.MaxSegments <= 0 {
		in.MaxSegments = defaults.MaxSegments
	}
	if in.MaxXMLDepth <= 0 {
		in.MaxXMLDepth = defaults.MaxXMLDepth
	}
	if in.MaxMetadataLength <= 0 {
		in.MaxMetadataLength = defaults.MaxMetadataLength
	}
	return in
}

func validateXMLShape(data []byte, maxDepth int) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	depth := 0
	rootCount := 0
	rootClosed := false
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("decode nzb XML: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				rootCount++
				if rootCount > 1 || rootClosed {
					return fmt.Errorf("nzb contains a trailing XML document")
				}
				if value.Name.Local != "nzb" {
					return fmt.Errorf("root element must be nzb")
				}
			}
			depth++
			if depth > maxDepth {
				return fmt.Errorf("nzb exceeds XML depth limit %d", maxDepth)
			}
		case xml.EndElement:
			depth--
			if depth < 0 {
				return fmt.Errorf("nzb XML is unbalanced")
			}
			if depth == 0 {
				rootClosed = true
			}
		case xml.CharData:
			if rootClosed && strings.TrimSpace(string(value)) != "" {
				return fmt.Errorf("nzb contains trailing content")
			}
		}
	}
	if rootCount != 1 || !rootClosed || depth != 0 {
		return fmt.Errorf("nzb must contain one complete XML document")
	}
	return nil
}

// NormalizeMessageID accepts common bracketed and unbracketed representations
// and returns exactly one pair of angle brackets.
func NormalizeMessageID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	if value == "" || strings.ContainsAny(value, "<>") || strings.Count(value, "@") != 1 {
		return "", fmt.Errorf("message ID is malformed")
	}
	parts := strings.SplitN(value, "@", 2)
	if strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("message ID is malformed")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", fmt.Errorf("message ID contains whitespace or control characters")
		}
	}
	return "<" + value + ">", nil
}

func validNewsgroup(value string) bool {
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '<' || r == '>' {
			return false
		}
	}
	return true
}

// SubjectFilename extracts a useful display name without claiming that an
// obfuscated subject is a verified source filename.
func SubjectFilename(subject string) string {
	subject = strings.TrimSpace(subject)
	if first := strings.IndexByte(subject, '"'); first >= 0 {
		if second := strings.IndexByte(subject[first+1:], '"'); second >= 0 {
			return strings.TrimSpace(subject[first+1 : first+1+second])
		}
	}
	lower := strings.ToLower(subject)
	if index := strings.Index(lower, " yenc"); index > 0 {
		subject = strings.TrimSpace(subject[:index])
	}
	return subject
}

func SortedSegments(file File) []Segment {
	out := append([]Segment(nil), file.Segments...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}
