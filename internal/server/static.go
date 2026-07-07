package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// spaHandler serves a built dashboard directory, falling back to index.html
// for client-side routes (anything without a file extension that doesn't
// exist on disk).
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, index)
			return
		}
		fs.ServeHTTP(w, r)
	})
}
