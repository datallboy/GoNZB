package nzb

import "encoding/xml"

type Model struct {
	XMLName xml.Name `xml:"nzb"`
	Files   []File   `xml:"file"`
}

type File struct {
	Subject  string    `xml:"subject,attr"`
	Poster   string    `xml:"poster,attr"`
	Groups   []string  `xml:"groups>group"`
	Segments []Segment `xml:"segments>segment"`
}

type Segment struct {
	XMLName   xml.Name `xml:"segment"`
	Number    int      `xml:"number,attr"`
	Bytes     int64    `xml:"bytes,attr"`
	MessageID string   `xml:",chardata"`
}
