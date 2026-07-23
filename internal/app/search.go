package app

type SearchRequest struct {
	Type string

	Query      string
	Categories []int
	Limit      int

	IMDbID   string
	TVDBID   string
	TVMazeID string
	RageID   string
	Season   string
	Episode  string
	Genre    string
}
