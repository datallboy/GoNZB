package domain

import "encoding/xml"

type NZB struct {
	Files []NZBFile `xml:"file"`
}

type NZBFile struct {
	Subject  string       `xml:"subject,attr"`
	Poster   string       `xml:"poster,attr"`
	Groups   []string     `xml:"groups>group"`
	Segments []NZBSegment `xml:"segments>segment"`
}

type NZBSegment struct {
	XMLName   xml.Name `xml:"segment"`
	Number    int      `xml:"number,attr"`
	Bytes     int64    `xml:"bytes,attr"`
	MessageID string   `xml:",chardata"`
}

func (f *NZBFile) TotalSize() int64 {
	var total int64
	for _, s := range f.Segments {
		total += int64(s.Bytes)
	}
	return total
}
