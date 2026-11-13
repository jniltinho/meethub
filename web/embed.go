// Package web embeds the built SPA (frontend/ → web/dist) into the binary.
// Only web/dist/.gitkeep is versioned; the "all:" prefix includes it so the
// embed pattern matches on a fresh clone, before the frontend is built.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the SPA files rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil { // only possible if the embed directive changes
		panic(err)
	}
	return sub
}
