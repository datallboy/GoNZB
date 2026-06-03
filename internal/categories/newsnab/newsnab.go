package newsnab

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Category struct {
	ID          int
	ParentID    int
	Name        string
	Slug        string
	Description string
}

type RootCategory struct {
	Category
	Subcategories []Category
}

type ReleaseAttributes struct {
	Classification    string
	ExternalMediaType string
	TMDBID            int64
	TVDBID            int64
	SeasonNumber      int
	EpisodeNumber     int
	PrimaryResolution string
	PrimaryAudioCodec string
	Title             string
	SourceTitle       string
	DeobfuscatedTitle string
	MatchedMediaTitle string
	TitleSource       string
	PredbCategory     string
	PredbGenre        string
	Poster            string
}

type Resolution struct {
	ID            int
	Name          string
	RootID        int
	RootName      string
	RootSlug      string
	SubcategoryID int
	Subcategory   string
	SubSlug       string
}

const (
	ConsoleRoot = 1000
	MoviesRoot  = 2000
	AudioRoot   = 3000
	PCRoot      = 4000
	TVRoot      = 5000
	XXXRoot     = 6000
	BooksRoot   = 7000
	OtherRoot   = 8000

	ConsoleNDS      = 1010
	ConsolePSP      = 1020
	ConsoleWii      = 1030
	ConsoleSwitch   = 1035
	ConsoleXbox     = 1040
	ConsoleXbox360  = 1050
	ConsolePS3      = 1080
	ConsoleXboxOne  = 1090
	ConsolePS4      = 1100
	MoviesForeign   = 2010
	MoviesOther     = 2020
	MoviesSD        = 2030
	MoviesHD        = 2040
	MoviesUHD       = 2045
	MoviesBluRay    = 2050
	MoviesThreeD    = 2060
	AudioMP3        = 3010
	AudioVideo      = 3020
	AudioAudiobook  = 3030
	AudioLossless   = 3040
	AudioPodcast    = 3050
	PC0Day          = 4010
	PCISO           = 4020
	PCMac           = 4030
	PCMobileOther   = 4040
	PCGames         = 4050
	PCMobileIOS     = 4060
	PCMobileAndroid = 4070
	PC3DModels      = 4080
	TVForeign       = 5020
	TVSD            = 5030
	TVHD            = 5040
	TVUHD           = 5045
	TVOther         = 5050
	TVSport         = 5060
	TVAnime         = 5070
	TVDocumentary   = 5080
	XXXDVD          = 6010
	XXXWMV          = 6020
	XXXSD           = 6030
	XXXHD           = 6040
	XXXUHD          = 6045
	XXXPack         = 6050
	XXXImgSet       = 6060
	XXXOther        = 6070
	BooksMags       = 7010
	BooksEbook      = 7020
	BooksComics     = 7030
	OtherMisc       = 8010
)

var roots = []RootCategory{
	{
		Category: Category{ID: ConsoleRoot, Name: "Console", Slug: "console"},
		Subcategories: []Category{
			{ID: ConsoleNDS, ParentID: ConsoleRoot, Name: "NDS", Slug: "nds"},
			{ID: ConsolePSP, ParentID: ConsoleRoot, Name: "PSP", Slug: "psp"},
			{ID: ConsoleWii, ParentID: ConsoleRoot, Name: "Wii", Slug: "wii"},
			{ID: ConsoleSwitch, ParentID: ConsoleRoot, Name: "Switch", Slug: "switch"},
			{ID: ConsoleXbox, ParentID: ConsoleRoot, Name: "Xbox", Slug: "xbox"},
			{ID: ConsoleXbox360, ParentID: ConsoleRoot, Name: "Xbox 360", Slug: "xbox-360"},
			{ID: ConsolePS3, ParentID: ConsoleRoot, Name: "PS3", Slug: "ps3"},
			{ID: ConsoleXboxOne, ParentID: ConsoleRoot, Name: "Xbox One", Slug: "xbox-one"},
			{ID: ConsolePS4, ParentID: ConsoleRoot, Name: "PS4", Slug: "ps4"},
		},
	},
	{
		Category: Category{ID: MoviesRoot, Name: "Movies", Slug: "movies"},
		Subcategories: []Category{
			{ID: MoviesForeign, ParentID: MoviesRoot, Name: "Foreign", Slug: "foreign"},
			{ID: MoviesOther, ParentID: MoviesRoot, Name: "Other", Slug: "other"},
			{ID: MoviesSD, ParentID: MoviesRoot, Name: "SD", Slug: "sd"},
			{ID: MoviesHD, ParentID: MoviesRoot, Name: "HD", Slug: "hd"},
			{ID: MoviesUHD, ParentID: MoviesRoot, Name: "UHD", Slug: "uhd"},
			{ID: MoviesBluRay, ParentID: MoviesRoot, Name: "BluRay", Slug: "bluray"},
			{ID: MoviesThreeD, ParentID: MoviesRoot, Name: "3D", Slug: "3d"},
		},
	},
	{
		Category: Category{ID: AudioRoot, Name: "Audio", Slug: "audio"},
		Subcategories: []Category{
			{ID: AudioMP3, ParentID: AudioRoot, Name: "MP3", Slug: "mp3"},
			{ID: AudioVideo, ParentID: AudioRoot, Name: "Video", Slug: "video"},
			{ID: AudioAudiobook, ParentID: AudioRoot, Name: "Audiobook", Slug: "audiobook"},
			{ID: AudioLossless, ParentID: AudioRoot, Name: "Lossless", Slug: "lossless"},
			{ID: AudioPodcast, ParentID: AudioRoot, Name: "Podcast", Slug: "podcast"},
		},
	},
	{
		Category: Category{ID: PCRoot, Name: "PC", Slug: "pc"},
		Subcategories: []Category{
			{ID: PC0Day, ParentID: PCRoot, Name: "0day", Slug: "0day"},
			{ID: PCISO, ParentID: PCRoot, Name: "ISO", Slug: "iso"},
			{ID: PCMac, ParentID: PCRoot, Name: "Mac", Slug: "mac"},
			{ID: PCMobileOther, ParentID: PCRoot, Name: "Mobile-Other", Slug: "mobile-other"},
			{ID: PCGames, ParentID: PCRoot, Name: "Games", Slug: "games"},
			{ID: PCMobileIOS, ParentID: PCRoot, Name: "Mobile-iOS", Slug: "mobile-ios"},
			{ID: PCMobileAndroid, ParentID: PCRoot, Name: "Mobile-Android", Slug: "mobile-android"},
			{ID: PC3DModels, ParentID: PCRoot, Name: "3dModels", Slug: "3dmodels", Description: "3dprint stls"},
		},
	},
	{
		Category: Category{ID: TVRoot, Name: "TV", Slug: "tv"},
		Subcategories: []Category{
			{ID: TVForeign, ParentID: TVRoot, Name: "Foreign", Slug: "foreign"},
			{ID: TVSD, ParentID: TVRoot, Name: "SD", Slug: "sd"},
			{ID: TVHD, ParentID: TVRoot, Name: "HD", Slug: "hd"},
			{ID: TVUHD, ParentID: TVRoot, Name: "UHD", Slug: "uhd"},
			{ID: TVOther, ParentID: TVRoot, Name: "Other", Slug: "other"},
			{ID: TVSport, ParentID: TVRoot, Name: "Sport", Slug: "sport"},
			{ID: TVAnime, ParentID: TVRoot, Name: "Anime", Slug: "anime"},
			{ID: TVDocumentary, ParentID: TVRoot, Name: "Documentary", Slug: "documentary"},
		},
	},
	{
		Category: Category{ID: XXXRoot, Name: "XXX", Slug: "xxx"},
		Subcategories: []Category{
			{ID: XXXDVD, ParentID: XXXRoot, Name: "DVD", Slug: "dvd"},
			{ID: XXXWMV, ParentID: XXXRoot, Name: "WMV", Slug: "wmv"},
			{ID: XXXSD, ParentID: XXXRoot, Name: "SD", Slug: "sd"},
			{ID: XXXHD, ParentID: XXXRoot, Name: "HD", Slug: "hd"},
			{ID: XXXUHD, ParentID: XXXRoot, Name: "UHD", Slug: "uhd"},
			{ID: XXXPack, ParentID: XXXRoot, Name: "Pack", Slug: "pack"},
			{ID: XXXImgSet, ParentID: XXXRoot, Name: "ImgSet", Slug: "imgset"},
			{ID: XXXOther, ParentID: XXXRoot, Name: "Other", Slug: "other"},
		},
	},
	{
		Category: Category{ID: BooksRoot, Name: "Books", Slug: "books"},
		Subcategories: []Category{
			{ID: BooksMags, ParentID: BooksRoot, Name: "Mags", Slug: "mags"},
			{ID: BooksEbook, ParentID: BooksRoot, Name: "Ebook", Slug: "ebook"},
			{ID: BooksComics, ParentID: BooksRoot, Name: "Comics", Slug: "comics"},
		},
	},
	{
		Category: Category{ID: OtherRoot, Name: "Other", Slug: "other"},
		Subcategories: []Category{
			{ID: OtherMisc, ParentID: OtherRoot, Name: "Misc", Slug: "misc"},
		},
	},
}

var byID map[int]Category
var subcategoriesByRoot map[int][]int

var (
	seasonEpisodeRE = regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`)
	xEpisodeRE      = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
	dailyEpisodeRE  = regexp.MustCompile(`\b20\d{2}[ ._-]\d{2}[ ._-]\d{2}\b`)
)

func init() {
	byID = make(map[int]Category, 64)
	subcategoriesByRoot = make(map[int][]int, len(roots))
	for _, root := range roots {
		byID[root.ID] = root.Category
		ids := make([]int, 0, len(root.Subcategories)+1)
		ids = append(ids, root.ID)
		for _, sub := range root.Subcategories {
			byID[sub.ID] = sub
			ids = append(ids, sub.ID)
		}
		subcategoriesByRoot[root.ID] = ids
	}
}

func Roots() []RootCategory {
	out := make([]RootCategory, len(roots))
	copy(out, roots)
	return out
}

func Lookup(id int) (Category, bool) {
	cat, ok := byID[id]
	return cat, ok
}

func ParentID(id int) int {
	cat, ok := Lookup(id)
	if !ok {
		return OtherRoot
	}
	if cat.ParentID == 0 {
		return cat.ID
	}
	return cat.ParentID
}

func Root(id int) Category {
	rootID := ParentID(id)
	cat, ok := Lookup(rootID)
	if !ok {
		return Category{ID: OtherRoot, Name: "Other", Slug: "other"}
	}
	return cat
}

func DisplayName(id int) string {
	cat, ok := Lookup(id)
	if !ok {
		return "Other > Misc"
	}
	if cat.ParentID == 0 {
		return cat.Name
	}
	return fmt.Sprintf("%s > %s", Root(id).Name, cat.Name)
}

func ParseID(raw string) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	_, ok := Lookup(id)
	return id, ok
}

func ParseName(raw string) (int, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return 0, false
	}
	raw = strings.ReplaceAll(raw, ">", " ")
	raw = strings.ReplaceAll(raw, "-", " ")
	raw = strings.Join(strings.Fields(raw), " ")
	for _, root := range roots {
		if normalizeLookupName(root.Name) == raw || root.Slug == raw {
			return root.ID, true
		}
		for _, sub := range root.Subcategories {
			if normalizeLookupName(root.Name+" "+sub.Name) == raw ||
				normalizeLookupName(sub.Name) == raw ||
				sub.Slug == raw {
				return sub.ID, true
			}
		}
	}
	return 0, false
}

func IDsForBrowse(categorySlug, subcategorySlug string) []int {
	categorySlug = strings.ToLower(strings.TrimSpace(categorySlug))
	subcategorySlug = strings.ToLower(strings.TrimSpace(subcategorySlug))
	if categorySlug == "" {
		return nil
	}
	for _, root := range roots {
		if root.Slug != categorySlug {
			continue
		}
		if subcategorySlug == "" || subcategorySlug == "all" {
			out := make([]int, len(subcategoriesByRoot[root.ID]))
			copy(out, subcategoriesByRoot[root.ID])
			return out
		}
		for _, sub := range root.Subcategories {
			if sub.Slug == subcategorySlug {
				return []int{sub.ID}
			}
		}
		return nil
	}
	return nil
}

func CategoryIDs() []int {
	out := make([]int, 0, len(byID))
	for id := range byID {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func ResolveReleaseCategory(in ReleaseAttributes) Resolution {
	combined := normalizeCombined(in.Title, in.SourceTitle, in.DeobfuscatedTitle, in.MatchedMediaTitle, in.PredbCategory, in.PredbGenre, in.Poster)
	classification := strings.ToLower(strings.TrimSpace(in.Classification))
	externalMediaType := strings.ToLower(strings.TrimSpace(in.ExternalMediaType))
	primaryAudioCodec := strings.ToLower(strings.TrimSpace(in.PrimaryAudioCodec))
	predbCategory := strings.ToLower(strings.TrimSpace(in.PredbCategory))
	episodic := hasEpisodeEvidence(in, combined)
	tvStructured := in.TVDBID > 0 || externalMediaType == "tv" || in.SeasonNumber > 0 || in.EpisodeNumber > 0 || episodic
	movieStructured := in.TMDBID > 0 || externalMediaType == "movie"

	switch {
	case classification == "xxx" || looksXXX(combined):
		return buildResolution(resolveXXXCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case looksConsole(combined):
		return buildResolution(resolveConsoleCategory(combined))
	case tvStructured:
		return buildResolution(resolveTVCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case movieStructured:
		return buildResolution(resolveMovieCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case predbLooksTV(predbCategory):
		return buildResolution(resolveTVCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case predbLooksMovie(predbCategory):
		return buildResolution(resolveMovieCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case looksTV(classification, combined):
		return buildResolution(resolveTVCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case looksMovie(classification, combined):
		return buildResolution(resolveMovieCategory(combined, normalizedResolutionID(in.PrimaryResolution)))
	case looksAudio(classification, externalMediaType, combined, primaryAudioCodec):
		return buildResolution(resolveAudioCategory(combined, primaryAudioCodec))
	case looksBooks(classification, combined):
		return buildResolution(resolveBooksCategory(combined))
	case looksPC(combined):
		return buildResolution(resolvePCCategory(combined))
	default:
		return buildResolution(OtherMisc)
	}
}

func buildResolution(id int) Resolution {
	cat, ok := Lookup(id)
	if !ok {
		cat = Category{ID: OtherMisc, ParentID: OtherRoot, Name: "Misc", Slug: "misc"}
	}
	root := Root(id)
	result := Resolution{
		ID:       id,
		Name:     DisplayName(id),
		RootID:   root.ID,
		RootName: root.Name,
		RootSlug: root.Slug,
	}
	if cat.ParentID != 0 {
		result.SubcategoryID = cat.ID
		result.Subcategory = cat.Name
		result.SubSlug = cat.Slug
	}
	return result
}

func normalizeCombined(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		replacer := strings.NewReplacer(".", " ", "_", " ", "-", " ", "/", " ", "\\", " ", "(", " ", ")", " ", "[", " ", "]", " ")
		part = replacer.Replace(part)
		clean = append(clean, part)
	}
	return " " + strings.Join(clean, " ") + " "
}

func normalizeLookupName(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, ">", " ")
	raw = strings.ReplaceAll(raw, "-", " ")
	return strings.Join(strings.Fields(raw), " ")
}

func hasAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, " "+needle+" ") {
			return true
		}
	}
	return false
}

func normalizedResolutionID(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "2160p", "4k", "uhd":
		return 2
	case "720p", "1080p":
		return 1
	default:
		return 0
	}
}

func looksXXX(combined string) bool {
	return hasAny(combined, "xxx", "porn", "nsfw", "onlyfans", "brazzers", "playboy", "erotica")
}

func looksConsole(combined string) bool {
	return hasAny(combined, "ps3", "ps4", "ps5", "playstation", "xbox", "x360", "xboxone", "switch", "nintendo", "wii", "psp", "nds", "3ds")
}

func looksTV(classification, combined string) bool {
	if classification == "tv" {
		return true
	}
	return hasEpisodePattern(combined)
}

func looksMovie(classification, combined string) bool {
	if classification == "movie" {
		return true
	}
	return hasAny(combined, "bluray", "bdrip", "dvdrip", "remux") || hasLikelyMovieYear(combined)
}

func looksAudio(classification, externalMediaType, combined, primaryAudioCodec string) bool {
	if classification == "audio" || externalMediaType == "audio" {
		return true
	}
	return primaryAudioCodec != "" || hasAny(combined, "flac", "mp3", "album", "discography", "podcast", "audiobook")
}

func looksBooks(classification, combined string) bool {
	if classification == "ebook" {
		return true
	}
	return hasAny(combined, "ebook", "epub", "mobi", "pdf", "comic", "magazine", "manga")
}

func looksPC(combined string) bool {
	return hasAny(combined, "windows", "mac", "android", "ios", "ipa", "apk", "game", "steam", "software", "0day", "trainer", "stl")
}

func resolveMovieCategory(combined string, resolution int) int {
	switch {
	case hasAny(combined, "3d"):
		return MoviesThreeD
	case hasAny(combined, "foreign", "french", "german", "spanish", "ita", "nlsubs"):
		return MoviesForeign
	case hasAny(combined, "bluray", "blu ray", "bdrip", "bdremux"):
		if resolution == 0 {
			return MoviesBluRay
		}
	}
	switch resolution {
	case 2:
		return MoviesUHD
	case 1:
		return MoviesHD
	default:
		if hasAny(combined, "dvdrip", "xvid", "480p", "576p") {
			return MoviesSD
		}
		return MoviesOther
	}
}

func resolveTVCategory(combined string, resolution int) int {
	switch {
	case hasAny(combined, "anime"):
		return TVAnime
	case hasAny(combined, "documentary", "docu"):
		return TVDocumentary
	case hasAny(combined, "sport", "sports", "ufc", "wwe", "formula", "motogp", "nba", "nfl", "mlb", "nhl"):
		return TVSport
	case hasAny(combined, "foreign", "german", "french", "spanish", "nlsubs"):
		return TVForeign
	}
	switch resolution {
	case 2:
		return TVUHD
	case 1:
		return TVHD
	default:
		if hasAny(combined, "480p", "576p") {
			return TVSD
		}
		return TVOther
	}
}

func hasEpisodeEvidence(in ReleaseAttributes, combined string) bool {
	if in.SeasonNumber > 0 || in.EpisodeNumber > 0 {
		return true
	}
	return hasEpisodePattern(combined)
}

func hasEpisodePattern(combined string) bool {
	return seasonEpisodeRE.MatchString(combined) || xEpisodeRE.MatchString(combined) || dailyEpisodeRE.MatchString(combined)
}

func hasLikelyMovieYear(combined string) bool {
	for year := 1950; year <= 2099; year++ {
		if strings.Contains(combined, fmt.Sprintf(" %d ", year)) {
			return true
		}
	}
	return false
}

func predbLooksTV(category string) bool {
	return strings.HasPrefix(category, "tv")
}

func predbLooksMovie(category string) bool {
	return strings.HasPrefix(category, "movies") || strings.HasPrefix(category, "movie")
}

func resolveConsoleCategory(combined string) int {
	switch {
	case hasAny(combined, "switch", "nintendo switch"):
		return ConsoleSwitch
	case hasAny(combined, "ps4", "playstation 4"):
		return ConsolePS4
	case hasAny(combined, "ps3", "playstation 3"):
		return ConsolePS3
	case hasAny(combined, "xbox one", "xbone"):
		return ConsoleXboxOne
	case hasAny(combined, "xbox 360", "x360"):
		return ConsoleXbox360
	case hasAny(combined, "xbox"):
		return ConsoleXbox
	case hasAny(combined, "wii"):
		return ConsoleWii
	case hasAny(combined, "psp"):
		return ConsolePSP
	case hasAny(combined, "nds", "3ds"):
		return ConsoleNDS
	default:
		return ConsoleRoot
	}
}

func resolveAudioCategory(combined, primaryAudioCodec string) int {
	switch {
	case hasAny(combined, "audiobook"):
		return AudioAudiobook
	case hasAny(combined, "podcast"):
		return AudioPodcast
	case strings.Contains(primaryAudioCodec, "flac") || hasAny(combined, "flac", "lossless"):
		return AudioLossless
	case strings.Contains(primaryAudioCodec, "mp3") || hasAny(combined, "mp3"):
		return AudioMP3
	case hasAny(combined, "music video", "concert"):
		return AudioVideo
	default:
		return AudioRoot
	}
}

func resolveBooksCategory(combined string) int {
	switch {
	case hasAny(combined, "comic", "comics", "manga"):
		return BooksComics
	case hasAny(combined, "magazine", "mags"):
		return BooksMags
	default:
		return BooksEbook
	}
}

func resolvePCCategory(combined string) int {
	switch {
	case hasAny(combined, "android", "apk"):
		return PCMobileAndroid
	case hasAny(combined, "ios", "iphone", "ipad", "ipa"):
		return PCMobileIOS
	case hasAny(combined, "mobile"):
		return PCMobileOther
	case hasAny(combined, "mac", "osx", "macos"):
		return PCMac
	case hasAny(combined, "game", "steam", "gog", "repack"):
		return PCGames
	case hasAny(combined, "iso"):
		return PCISO
	case hasAny(combined, "stl", "3d print", "3dprint"):
		return PC3DModels
	case hasAny(combined, "0day"):
		return PC0Day
	default:
		return PCRoot
	}
}

func resolveXXXCategory(combined string, resolution int) int {
	switch {
	case hasAny(combined, "imgset", "photoset"):
		return XXXImgSet
	case hasAny(combined, "pack"):
		return XXXPack
	case hasAny(combined, "wmv"):
		return XXXWMV
	case hasAny(combined, "dvd"):
		return XXXDVD
	}
	switch resolution {
	case 2:
		return XXXUHD
	case 1:
		return XXXHD
	default:
		if hasAny(combined, "480p", "576p") {
			return XXXSD
		}
		return XXXOther
	}
}
