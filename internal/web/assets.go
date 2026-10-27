package web

import (
	"embed"
	"io/fs"
)

// dist holds the Vite build. Only .keep is tracked, so the package always
// compiles; without a build the index answers 503.
//
//go:embed all:dist
var frontend embed.FS

// Assets returns the embedded frontend rooted at dist.
func Assets() (fs.FS, error) { return fs.Sub(frontend, "dist") }
