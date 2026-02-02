package nzb

// GetCategoryName maps Newznab IDs to human-readable strings.
func GetCategoryName(id string) string {
	mapping := map[string]string{
		"1000": "Console",
		"2000": "Movies",
		"2030": "Movies > SD",
		"2040": "Movies > HD",
		"2045": "Movies > UHD",
		"3000": "Audio",
		"4000": "PC",
		"5000": "TV",
		"5030": "TV > SD",
		"5040": "TV > HD",
		"5045": "TV > UHD",
		"6000": "XXX",
		"7000": "Other",
	}
	if name, ok := mapping[id]; ok {
		return name
	}
	return "Other" // Fallback
}
