package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var embedded embed.FS

func FS() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}
