package controllers

import "strings"

type sabAPIRequest struct {
	Mode        string `query:"mode" form:"mode"`
	Output      string `query:"output" form:"output"`
	Name        string `query:"name" form:"name"`
	Value       string `query:"value" form:"value"`
	NZOID       string `query:"nzo_id" form:"nzo_id"`
	Limit       string `query:"limit" form:"limit"`
	Start       string `query:"start" form:"start"`
	Search      string `query:"search" form:"search"`
	Category    string `query:"cat" form:"cat"`
	CategoryAlt string `query:"category" form:"category"`
	Archive     string `query:"archive" form:"archive"`
	DelFiles    string `query:"del_files" form:"del_files"`

	Section string `query:"section" form:"section"`
	Keyword string `query:"keyword" form:"keyword"`

	SkipDashboard        string `query:"skip_dashboard" form:"skip_dashboard"`
	CalculatePerformance string `query:"calculate_performance" form:"calculate_performance"`

	NZBURL   string `query:"name" form:"name"`
	NZBName  string `query:"nzbname" form:"nzbname"`
	Priority string `query:"priority" form:"priority"`
	PP       string `query:"pp" form:"pp"`
	Script   string `query:"script" form:"script"`
}

func (r *sabAPIRequest) normalize() {
	r.Mode = strings.TrimSpace(strings.ToLower(r.Mode))
	r.Output = strings.TrimSpace(strings.ToLower(r.Output))
	r.Name = strings.TrimSpace(r.Name)
	r.Value = strings.TrimSpace(r.Value)
	r.NZOID = strings.TrimSpace(r.NZOID)
	r.Limit = strings.TrimSpace(r.Limit)
	r.Start = strings.TrimSpace(r.Start)
	r.Search = strings.TrimSpace(r.Search)
	r.Category = strings.TrimSpace(r.Category)
	r.CategoryAlt = strings.TrimSpace(r.CategoryAlt)
	r.Archive = strings.TrimSpace(r.Archive)
	r.DelFiles = strings.TrimSpace(r.DelFiles)

	if r.Category == "" {
		r.Category = r.CategoryAlt
	}

	r.Section = strings.TrimSpace(strings.ToLower(r.Section))
	r.Keyword = strings.TrimSpace(strings.ToLower(r.Keyword))
	r.SkipDashboard = strings.TrimSpace(r.SkipDashboard)
	r.CalculatePerformance = strings.TrimSpace(r.CalculatePerformance)
	r.NZBName = strings.TrimSpace(r.NZBName)
	r.Priority = strings.TrimSpace(r.Priority)
	r.PP = strings.TrimSpace(r.PP)
	r.Script = strings.TrimSpace(r.Script)

	if r.Output == "" {
		r.Output = "json"
	}
}

func (r sabAPIRequest) addURL() string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}

func (r sabAPIRequest) deleteTarget() string {
	if r.Value != "" {
		return r.Value
	}
	if r.NZOID != "" {
		return r.NZOID
	}
	return r.Name
}

func (r sabAPIRequest) filesTarget() string {
	if r.Value != "" {
		return r.Value
	}
	if r.NZOID != "" {
		return r.NZOID
	}
	return r.Name
}

type sabStatusResponse struct {
	Status bool   `json:"status"`
	Error  string `json:"error,omitempty"`
}

type sabAddResponse struct {
	Status bool     `json:"status"`
	NZOIDs []string `json:"nzo_ids,omitempty"`
	Error  string   `json:"error,omitempty"`
}

type sabQueueResponse struct {
	Queue sabQueueData `json:"queue"`
}

type sabQueueData struct {
	Status         string         `json:"status"`
	Speedlimit     string         `json:"speedlimit"`
	SpeedlimitAbs  string         `json:"speedlimit_abs"`
	Paused         bool           `json:"paused"`
	PausedAll      bool           `json:"paused_all"`
	NoOfSlotsTotal int            `json:"noofslots_total"`
	NoOfSlots      int            `json:"noofslots"`
	Limit          int            `json:"limit"`
	Start          int            `json:"start"`
	TimeLeft       string         `json:"timeleft"`
	Speed          string         `json:"speed"`
	KBPerSec       string         `json:"kbpersec"`
	Size           string         `json:"size"`
	SizeLeft       string         `json:"sizeleft"`
	MB             string         `json:"mb"`
	MBLeft         string         `json:"mbleft"`
	Slots          []sabQueueSlot `json:"slots"`

	DiskSpace1      string `json:"diskspace1"`
	DiskSpace2      string `json:"diskspace2"`
	DiskSpaceTotal1 string `json:"diskspacetotal1"`
	DiskSpaceTotal2 string `json:"diskspacetotal2"`
	DiskSpace1Norm  string `json:"diskspace1_norm"`
	DiskSpace2Norm  string `json:"diskspace2_norm"`
	HaveWarnings    string `json:"have_warnings"`
	PauseInt        string `json:"pause_int"`
	LeftQuota       string `json:"left_quota"`
	Version         string `json:"version"`
	Finish          int    `json:"finish"`
	CacheArt        string `json:"cache_art"`
	CacheSize       string `json:"cache_size"`
	FinishAction    any    `json:"finishaction"`
	Quota           string `json:"quota"`
	HaveQuota       bool   `json:"have_quota"`
}

type sabQueueSlot struct {
	Status       string   `json:"status"`
	Index        int      `json:"index"`
	Password     string   `json:"password"`
	AvgAge       string   `json:"avg_age"`
	TimeAdded    int64    `json:"time_added"`
	Script       string   `json:"script"`
	DirectUnpack any      `json:"direct_unpack"`
	MB           string   `json:"mb"`
	MBLeft       string   `json:"mbleft"`
	MBMissing    string   `json:"mbmissing"`
	Size         string   `json:"size"`
	SizeLeft     string   `json:"sizeleft"`
	Filename     string   `json:"filename"`
	Labels       []string `json:"labels"`
	Priority     string   `json:"priority"`
	Category     string   `json:"cat"`
	TimeLeft     string   `json:"timeleft"`
	Percentage   string   `json:"percentage"`
	NZOID        string   `json:"nzo_id"`
	UnpackOpts   string   `json:"unpackopts"`
	Bytes        int64    `json:"bytes,omitempty"`
}

type sabHistoryResponse struct {
	Status  bool           `json:"status,omitempty"`
	Error   string         `json:"error,omitempty"`
	History sabHistoryData `json:"history"`
}

type sabHistoryData struct {
	NoOfSlots int              `json:"noofslots"`
	Slots     []sabHistorySlot `json:"slots"`
}

type sabHistorySlot struct {
	NZOID       string `json:"nzo_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	FailMessage string `json:"fail_message,omitempty"`
	Completed   string `json:"completed,omitempty"`
	Category    string `json:"category,omitempty"`
	SizeBytes   int64  `json:"bytes"`
	Storage     string `json:"storage,omitempty"`
}

type sabFilesResponse struct {
	Status bool           `json:"status,omitempty"`
	Error  string         `json:"error,omitempty"`
	Files  []sabFileEntry `json:"files"`
}

type sabFileEntry struct {
	Filename string `json:"filename"`
	Size     int64  `json:"bytes"`
	Subject  string `json:"subject,omitempty"`
	Date     int64  `json:"date,omitempty"`
}

type sabVersionResponse struct {
	Version string `json:"version"`
}

type sabCategoriesResponse struct {
	Categories []string `json:"categories"`
}

type sabConfigResponse struct {
	Config sabConfigData `json:"config"`
}

type sabConfigData struct {
	Misc       sabConfigMisc       `json:"misc"`
	Categories []sabConfigCategory `json:"categories"`
}

type sabConfigMisc struct {
	DownloadDir  string `json:"download_dir"`
	CompleteDir  string `json:"complete_dir"`
	TVSorting    int    `json:"tv_sort"`
	MovieSorting int    `json:"movie_sort"`
}

type sabConfigCategory struct {
	Name     string `json:"name"`
	Order    int    `json:"order"`
	PP       string `json:"pp"`
	Script   string `json:"script"`
	Dir      string `json:"dir"`
	Newzbin  string `json:"newzbin"`
	Priority string `json:"priority"`
}

type sabFullStatusResponse struct {
	Status sabFullStatusData `json:"status"`
}

type sabFullStatusData struct {
	Status         string         `json:"status"`
	Speedlimit     string         `json:"speedlimit"`
	SpeedlimitAbs  string         `json:"speedlimit_abs"`
	Paused         bool           `json:"paused"`
	PausedAll      bool           `json:"paused_all"`
	NoOfSlotsTotal int            `json:"noofslots_total"`
	NoOfSlots      int            `json:"noofslots"`
	Limit          int            `json:"limit"`
	Start          int            `json:"start"`
	TimeLeft       string         `json:"timeleft"`
	Speed          string         `json:"speed"`
	KBPerSec       string         `json:"kbpersec"`
	Size           string         `json:"size"`
	SizeLeft       string         `json:"sizeleft"`
	MB             string         `json:"mb"`
	MBLeft         string         `json:"mbleft"`
	Slots          []sabQueueSlot `json:"slots"`

	DiskSpace1      string `json:"diskspace1"`
	DiskSpace2      string `json:"diskspace2"`
	DiskSpaceTotal1 string `json:"diskspacetotal1"`
	DiskSpaceTotal2 string `json:"diskspacetotal2"`
	DiskSpace1Norm  string `json:"diskspace1_norm"`
	DiskSpace2Norm  string `json:"diskspace2_norm"`
	HaveWarnings    string `json:"have_warnings"`
	PauseInt        string `json:"pause_int"`
	LeftQuota       string `json:"left_quota"`
	Version         string `json:"version"`
	Finish          int    `json:"finish"`
	CacheArt        string `json:"cache_art"`
	CacheSize       string `json:"cache_size"`
	FinishAction    any    `json:"finishaction"`
	Quota           string `json:"quota"`
	HaveQuota       bool   `json:"have_quota"`

	LocalIPv4        string            `json:"localipv4"`
	IPv6             any               `json:"ipv6"`
	PublicIPv4       any               `json:"publicipv4"`
	DNSLookup        string            `json:"dnslookup"`
	Folders          []string          `json:"folders"`
	CPUModel         string            `json:"cpumodel"`
	Pystone          int               `json:"pystone"`
	LoadAvg          string            `json:"loadavg"`
	DownloadDir      string            `json:"downloaddir"`
	DownloadDirSpeed int               `json:"downloaddirspeed"`
	CompleteDir      string            `json:"completedir"`
	CompleteDirSpeed int               `json:"completedirspeed"`
	LogLevel         string            `json:"loglevel"`
	LogFile          string            `json:"logfile"`
	ConfigFn         string            `json:"configfn"`
	NT               bool              `json:"nt"`
	Darwin           bool              `json:"darwin"`
	ConfigHelpURI    string            `json:"confighelpuri"`
	Uptime           string            `json:"uptime"`
	ColorScheme      string            `json:"color_scheme"`
	WebDir           string            `json:"webdir"`
	ActiveLang       string            `json:"active_lang"`
	RestartReq       bool              `json:"restart_req"`
	PowerOptions     bool              `json:"power_options"`
	PPPauseEvent     bool              `json:"pp_pause_event"`
	PID              int               `json:"pid"`
	WebLogFile       any               `json:"weblogfile"`
	NewRelease       bool              `json:"new_release"`
	NewRelURL        any               `json:"new_rel_url"`
	Warnings         []map[string]any  `json:"warnings"`
	Servers          []sabStatusServer `json:"servers"`
}

type sabStatusServer struct {
	ServerName        string                      `json:"servername"`
	ServerTotalConn   int                         `json:"servertotalconn"`
	ServerSSL         int                         `json:"serverssl"`
	ServerActiveConn  int                         `json:"serveractiveconn"`
	ServerOptional    int                         `json:"serveroptional"`
	ServerActive      bool                        `json:"serveractive"`
	ServerError       string                      `json:"servererror"`
	ServerPriority    int                         `json:"serverpriority"`
	ServerBPS         string                      `json:"serverbps"`
	ServerConnections []sabStatusServerConnection `json:"serverconnections"`
}

type sabStatusServerConnection struct {
	ThreadNum int    `json:"thrdnum"`
	NZOName   string `json:"nzo_name"`
	NZFName   string `json:"nzf_name"`
	ArtName   string `json:"art_name"`
}
