package server

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dashFS embed.FS

// DashboardFS returns the embedded dashboard build, or nil when the dist
// directory only contains the placeholder (dashboard not built yet).
func DashboardFS() fs.FS {
	sub, err := fs.Sub(dashFS, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
