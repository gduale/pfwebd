// Package web serves the embedded single-page frontend.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Register mounts the static frontend at the mux root.
func Register(mux *http.ServeMux) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // impossible: path is constant and embedded
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
}
