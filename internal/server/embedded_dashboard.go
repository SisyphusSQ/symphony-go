package server

import (
	"embed"
	"io/fs"
)

//go:embed all:embedded_dashboard/dist
var embeddedDashboardFiles embed.FS

func embeddedDashboardFS() fs.FS {
	fsys, err := fs.Sub(embeddedDashboardFiles, "embedded_dashboard/dist")
	if err != nil {
		return nil
	}
	return fsys
}
