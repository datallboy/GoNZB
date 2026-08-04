package buildinfo

// Version and BuildTime are replaced by release builds through -ldflags.
var (
	Version   = "dev"
	BuildTime = "unknown"
)
