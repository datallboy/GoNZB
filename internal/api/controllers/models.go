package controllers

import "encoding/xml"

type NewznabCaps struct {
	XMLName    xml.Name   `xml:"caps"`
	Server     ServerInfo `xml:"server"`
	Categories []Category `xml:"categories>category"`
}

type ServerInfo struct {
	Version string `xml:"version,attr"`
	Title   string `xml:"title,attr"`
}

type Category struct {
	ID      int      `xml:"id,attr"`
	Name    string   `xml:"name,attr"`
	SubCats []SubCat `xml:"subcat"`
}

type SubCat struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

type NewznabRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel Channel  `xml:"channel"`
}

type Channel struct {
	Title       string    `xml:"title"`
	Description string    `xml:"description"`
	Items       []RSSItem `xml:"item"`
}

type RSSItem struct {
	Title     string    `xml:"title"`
	GUID      string    `xml:"guid"`
	Link      string    `xml:"link"`
	Category  string    `xml:"category"`
	PubDate   string    `xml:"pubDate"`
	Enclosure Enclosure `xml:"enclosure"`
}

type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}
