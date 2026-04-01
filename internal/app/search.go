package app

type SearchRequest struct {
	Type string

	Query string

	IMDbID   string
	TVDBID   string
	TVMazeID string
	RageID   string
	Season   string
	Episode  string
	Genre    string
}
