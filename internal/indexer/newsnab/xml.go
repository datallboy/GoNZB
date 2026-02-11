package newsnab

import (
	"encoding/xml"
	"strconv"
	"time"

	"github.com/datallboy/gonzb/internal/domain"
)

type RSSResponse struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	NS      string   `xml:"xmlns:newznab,attr"`
	Channel Channel  `xml:"channel"`
}

type Channel struct {
	Title       string       `xml:"title"`
	Description string       `xml:"description"`
	Link        string       `xml:"link"`
	Items       []Item       `xml:"item"`
	Response    ResponseInfo `xml:"newznab:response"`
}

type ResponseInfo struct {
	Offset int `xml:"offset,attr"`
	Total  int `xml:"total,attr"`
}

type Item struct {
	Title      string      `xml:"title"`
	GUID       GUID        `xml:"guid"`
	Link       string      `xml:"link"`
	PubDate    string      `xml:"pubDate"`
	Category   string      `xml:"category"`
	Enclosure  Enclosure   `xml:"enclosure"`
	Attributes []Attribute `xml:"attr"`
}

func (i Item) getSize() int64 {
	size := i.getAttribute("size")
	if size != "" {
		val, _ := strconv.ParseInt(size, 10, 64)
		return val
	}
	return 0
}

func (i Item) getCategory() string {
	cat := i.getAttribute("category")
	if cat != "" {
		return cat
	}
	return ""
}

func (i Item) getAttribute(name string) string {
	for _, a := range i.Attributes {
		if a.Name == name {
			return a.Value
		}
	}
	return ""
}

func (i Item) getPubishDate() time.Time {
	t, _ := time.Parse(time.RFC1123Z, i.PubDate)
	return t
}

func (i Item) ToRelease(sourceName string) *domain.Release {
	id := domain.GenerateCompositeID(sourceName, i.GUID.Value)

	res := &domain.Release{
		ID:          id,
		Title:       i.Title,
		GUID:        i.GUID.Value,
		DownloadURL: i.Link,
		Size:        i.getSize(),
		Source:      sourceName,
		PublishDate: i.getPubishDate(),
		Category:    i.getCategory(),
	}

	return res
}

type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type Attribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type GUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}
