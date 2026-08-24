// Package web embeds the Vite production build so the React workbench ships as
// one static binary. Run the web/ui build before compiling Go; make build does
// both steps.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// Assets returns the UI file tree rooted at index.html.
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		// The embedded tree is fixed at compile time, so this cannot fail.
		panic(err)
	}
	return sub
}
