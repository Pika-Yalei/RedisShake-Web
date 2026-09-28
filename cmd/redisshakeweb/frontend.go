package main

import (
	"io/fs"
	"net/http"
)

// Only page routes serve the app shell. Missing assets and API paths stay 404s.
func frontendHandler(assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	files := http.FileServer(http.FS(assets))
	page := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, assets, "index.html")
	}
	for _, route := range []string{
		"/{$}", "/tasks", "/tasks/new", "/tasks/{id}", "/tasks/{id}/edit",
		"/connections", "/connections/new", "/connections/{id}/edit",
	} {
		mux.HandleFunc("GET "+route, page)
	}
	mux.Handle("GET /", files)
	return mux
}
