// Package web embeds the built frontend (SPA) so OpenCuttles ships as a single
// binary with no separate static-asset deployment.
//
// The dist directory is populated by the build (the Makefile copies
// frontend/dist here before "go build"). A committed dist/.gitkeep keeps the
// embed directive valid even before the frontend has been built.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the embedded SPA filesystem rooted at dist. The second return
// value is false when no real build is embedded (e.g. only the .gitkeep
// placeholder), in which case the API runs without serving the UI.
func Assets() (fs.FS, bool) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
