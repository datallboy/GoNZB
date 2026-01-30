package controllers

import "encoding/xml"

// -- CAPABILITIES (t=caps) ---
type NewznabCaps struct {
	XMLName    xml.Name      `xml:"caps"`
	Server     ServerInfo    `xml:"server"`
	Limits     Limits        `xml:"limits"`
	Retention  Retention     `xml:"retention"`
	Categories []CapCategory `xml:"categories>category"`
}

type ServerInfo struct {
	Version string `xml:"version,attr"`
	Title   string `xml:"title,attr"`
}

type Limits struct {
	Max int `xml:"max,attr"`
}

type Retention struct {
	Days int `xml:"days,attr"`
}

type CapCategory struct {
	ID      int         `xml:"id,attr"`
	Name    string      `xml:"name,attr"`
	SubCats []CapSubCat `xml:"subcat"`
}

type CapSubCat struct {
	ID   int    `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

// -- SEARCH RESULTS (t=search/tv/movie)
type NewznabRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	NS      string   `xml:"xmlns:newznab,attr"`
	Channel Channel  `xml:"channel"`
}

type Channel struct {
	Title       string    `xml:"title"`
	Description string    `xml:"description"`
	Link        string    `xml:"link"`
	Items       []RSSItem `xml:"item"`
	Response    Response  `xml:"newznab:response"`
}

type RSSItem struct {
	Title      string    `xml:"title"`
	GUID       RSSGUID   `xml:"guid"`
	Link       string    `xml:"link"`
	Category   string    `xml:"category"`
	PubDate    string    `xml:"pubDate"`
	Enclosure  Enclosure `xml:"enclosure"`
	Attributes []Attr    `xml:"newznab:attr"`
}

type RSSGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type Attr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type Response struct {
	Offset int `xml:"offset,attr"`
	Total  int `xml:"total,attr"`
}
