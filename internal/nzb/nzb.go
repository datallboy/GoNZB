package nzb

import (
	"encoding/xml"
	"strings"
)

type Model struct {
	XMLName xml.Name `xml:"nzb"`
	Meta    []Meta   `xml:"head>meta"`
	Files   []File   `xml:"file"`
}

type Meta struct {
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

type File struct {
	Subject  string    `xml:"subject,attr"`
	Poster   string    `xml:"poster,attr"`
	Date     int64     `xml:"date,attr"`
	Groups   []string  `xml:"groups>group"`
	Segments []Segment `xml:"segments>segment"`
}

type Segment struct {
	XMLName   xml.Name `xml:"segment"`
	Number    int      `xml:"number,attr"`
	Bytes     int64    `xml:"bytes,attr"`
	MessageID string   `xml:",chardata"`
}

// Helper method to grab the password from the slice of Meta tags
func (n *Model) GetPassword() string {
	for _, m := range n.Meta {
		if m.Type == "password" {
			return strings.TrimSpace(m.Content)
		}
	}
	return ""
}
