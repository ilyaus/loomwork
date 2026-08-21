// Package web embeds the browser UI so the workbench ships as one static binary.
// The assets are hand-written ES modules and CSS with no build step, so a `go
// build` is the whole build: nothing regenerates or minifies these files.
package web

import (
	"embed"
	"io/fs"
)

//go:embed assets
var assets embed.FS

// Assets returns the UI file tree rooted at index.html.
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		// The embedded tree is fixed at compile time, so this cannot fail.
		panic(err)
	}
	return sub
}
