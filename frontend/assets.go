package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var bundled embed.FS

func Assets() fs.FS {
	f, e := fs.Sub(bundled, "dist")
	if e != nil {
		panic(e)
	}
	return f
}
