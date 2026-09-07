package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

// Hasil build React (PRD §4: ditanam ke binary, deploy tetap scp satu file).
// Folder web/dist di-commit ke repo — lihat web/.gitignore.
//
//go:embed web/dist
var webDistFS embed.FS

// newSPAHandler melayani aset dari web/dist. Path yang nggak nunjuk ke file
// mana pun dibalikin index.html — catch-all rute SPA (PRD aturan keras §8),
// biar refresh di halaman dalam nggak 404.
func newSPAHandler() http.Handler {
	dist, err := fs.Sub(webDistFS, "web/dist")
	if err != nil {
		panic(err) // web/dist di-embed saat compile; nggak mungkin gagal
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(r.URL.Path)
		if f, err := dist.Open(name[1:]); err == nil { // buang "/" di depan
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}
