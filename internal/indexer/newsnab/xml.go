package newsnab

import (
	"encoding/xml"
	"strconv"
	"time"

	"github.com/datallboy/gonzb/internal/indexer"
)

type RSSResponse struct {
	XMLName xml.Name `xml:"rss"`
	Channel Channel  `xml:"channel"`
}

type Channel struct {
	Items []Item `xml:"item"`
}

type Item struct {
	Title      string      `xml:"title"`
	Link       string      `xml:"link"`
	GUID       string      `xml:"guid"`
	PubDate    string      `xml:"pubDate"`
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

func (i Item) ToSearchResult(sourceName string) indexer.SearchResult {
	res := indexer.SearchResult{
		Title:       i.Title,
		GUID:        i.GUID,
		DownloadURL: i.Link,
		Size:        i.getSize(),
		Source:      sourceName,
		PublishDate: i.getPubishDate(),
		Category:    i.getCategory(),
	}
	res.SetCompositeID()
	return res
}

type Attribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
