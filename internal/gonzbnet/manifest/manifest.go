package manifest

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
)

const (
	Type       = "ResolutionManifest"
	BodySchema = "gonzbnet.ResolutionManifest/1.0"
	IDPrefix   = "man"
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

type ResolutionManifest struct {
	SchemaVersion string       `json:"schema_version"`
	Type          string       `json:"type"`
	ManifestID    string       `json:"manifest_id"`
	ReleaseID     string       `json:"release_id"`
	ManifestCore  ManifestCore `json:"manifest_core"`
	Compression   string       `json:"compression,omitempty"`
	Encrypted     bool         `json:"encrypted"`
}

type Request struct {
	SchemaVersion    string `json:"schema_version"`
	Type             string `json:"type"`
	RequestID        string `json:"request_id"`
	ManifestID       string `json:"manifest_id"`
	ReleaseID        string `json:"release_id"`
	PoolID           string `json:"pool_id"`
	RequestingNodeID string `json:"requesting_node_id"`
	Reason           string `json:"reason"`
	CreatedAt        string `json:"created_at"`
}

type Response struct {
	SchemaVersion string              `json:"schema_version"`
	Type          string              `json:"type"`
	RequestID     string              `json:"request_id"`
	Status        string              `json:"status"`
	Code          string              `json:"code,omitempty"`
	Message       string              `json:"message,omitempty"`
	ManifestEvent *events.SignedEvent `json:"manifest_event,omitempty"`
}

type ManifestCore struct {
	Groups   []string       `json:"groups"`
	Poster   string         `json:"poster,omitempty"`
	PostedAt string         `json:"posted_at,omitempty"`
	Files    []ManifestFile `json:"files"`
	PAR2     PAR2           `json:"par2"`
	Hashes   Hashes         `json:"hashes"`
	NZB      NZBInfo        `json:"nzb"`
}

type ManifestFile struct {
	Name      string            `json:"name"`
	Subject   string            `json:"subject"`
	Date      string            `json:"date"`
	SizeBytes int64             `json:"size_bytes"`
	Segments  []ManifestSegment `json:"segments"`
}

type ManifestSegment struct {
	Number    int    `json:"number"`
	Bytes     int64  `json:"bytes"`
	MessageID string `json:"message_id"`
}

type PAR2 struct {
	Present     bool `json:"present"`
	BaseFiles   int  `json:"base_files"`
	VolumeFiles int  `json:"volume_files"`
}

type Hashes struct {
	FileListHash    string `json:"file_list_hash,omitempty"`
	SegmentListHash string `json:"segment_list_hash,omitempty"`
}

type NZBInfo struct {
	Generator  string `json:"generator"`
	XMLCharset string `json:"xml_charset"`
}

func ComputeID(core ManifestCore) (string, []byte, error) {
	canonicalCore, err := canonical.Marshal(core)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonicalCore)
	return IDPrefix + "_" + strings.ToLower(base32NoPadding.EncodeToString(sum[:])), canonicalCore, nil
}

func Validate(in ResolutionManifest) ([]byte, error) {
	if strings.TrimSpace(in.SchemaVersion) != "1.0" {
		return nil, fmt.Errorf("unsupported manifest schema_version")
	}
	if strings.TrimSpace(in.Type) != Type {
		return nil, fmt.Errorf("unsupported manifest type")
	}
	if strings.TrimSpace(in.ReleaseID) == "" {
		return nil, fmt.Errorf("release_id is required")
	}
	if len(in.ManifestCore.Files) == 0 {
		return nil, fmt.Errorf("manifest_core.files is required")
	}
	expected, canonicalCore, err := ComputeID(in.ManifestCore)
	if err != nil {
		return nil, err
	}
	if expected != strings.TrimSpace(in.ManifestID) {
		return nil, fmt.Errorf("manifest_id mismatch")
	}
	for _, file := range in.ManifestCore.Files {
		if strings.TrimSpace(file.Subject) == "" && strings.TrimSpace(file.Name) == "" {
			return nil, fmt.Errorf("manifest file requires name or subject")
		}
		if len(file.Segments) == 0 {
			return nil, fmt.Errorf("manifest file requires segments")
		}
		for _, segment := range file.Segments {
			if segment.Number <= 0 || segment.Bytes < 0 || strings.TrimSpace(segment.MessageID) == "" {
				return nil, fmt.Errorf("manifest segment is invalid")
			}
		}
	}
	return canonicalCore, nil
}

func GenerateNZB(in ResolutionManifest) ([]byte, error) {
	if _, err := Validate(in); err != nil {
		return nil, err
	}
	type groupXML struct {
		Name string `xml:",chardata"`
	}
	type segmentXML struct {
		Bytes  int64  `xml:"bytes,attr"`
		Number int    `xml:"number,attr"`
		ID     string `xml:",chardata"`
	}
	type fileXML struct {
		Poster   string       `xml:"poster,attr"`
		Date     int64        `xml:"date,attr"`
		Subject  string       `xml:"subject,attr"`
		Groups   []groupXML   `xml:"groups>group"`
		Segments []segmentXML `xml:"segments>segment"`
	}
	type nzbXML struct {
		XMLName xml.Name  `xml:"nzb"`
		Xmlns   string    `xml:"xmlns,attr"`
		Files   []fileXML `xml:"file"`
	}

	groups := normalizeStrings(in.ManifestCore.Groups)
	sort.Strings(groups)
	doc := nzbXML{
		Xmlns: "http://www.newzbin.com/DTD/2003/nzb",
		Files: make([]fileXML, 0, len(in.ManifestCore.Files)),
	}
	for _, file := range in.ManifestCore.Files {
		segments := make([]segmentXML, 0, len(file.Segments))
		for _, segment := range file.Segments {
			segments = append(segments, segmentXML{
				Bytes:  segment.Bytes,
				Number: segment.Number,
				ID:     strings.TrimSpace(segment.MessageID),
			})
		}
		fileGroups := make([]groupXML, 0, len(groups))
		for _, group := range groups {
			fileGroups = append(fileGroups, groupXML{Name: group})
		}
		subject := firstNonBlank(file.Subject, file.Name, in.ReleaseID)
		poster := firstNonBlank(in.ManifestCore.Poster, "unknown")
		doc.Files = append(doc.Files, fileXML{
			Poster:   poster,
			Date:     parseManifestTime(file.Date, in.ManifestCore.PostedAt).Unix(),
			Subject:  subject,
			Groups:   fileGroups,
			Segments: segments,
		})
	}
	payload, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), payload...), nil
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseManifestTime(values ...string) time.Time {
	for _, value := range values {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}
