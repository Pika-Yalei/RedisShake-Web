package main

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFrontendAssetsAreEmbedded(t *testing.T) {
	for _, name := range []string{"static/index.html", "static/app.js", "static/toast.js", "static/select.js", "static/style.css", "static/redis-logo.svg"} {
		data, err := assets.ReadFile(name)
		if err != nil || len(data) == 0 {
			t.Fatalf("embedded asset %s: %v", name, err)
		}
	}
}

func TestFrontendPageRoutes(t *testing.T) {
	embedded, err := fs.Sub(assets, "static")
	if err != nil {
		t.Fatal(err)
	}
	for name, files := range map[string]fs.FS{"embedded": embedded, "development": os.DirFS("static")} {
		t.Run(name, func(t *testing.T) {
			handler := frontendHandler(files)
			index, err := fs.ReadFile(files, "index.html")
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/", "/tasks", "/tasks/new", "/tasks/task-123", "/connections", "/connections/new", "/connections/connection-123/edit"} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					r := httptest.NewRecorder()
					handler.ServeHTTP(r, httptest.NewRequest(method, path, nil))
					if r.Code != http.StatusOK || !strings.HasPrefix(r.Header().Get("Content-Type"), "text/html") {
						t.Fatalf("%s %s: status %d, content type %q", method, path, r.Code, r.Header().Get("Content-Type"))
					}
					if method == http.MethodGet && !bytes.Equal(r.Body.Bytes(), index) {
						t.Fatalf("%s: did not serve the app shell", path)
					}
					if method == http.MethodHead && r.Body.Len() != 0 {
						t.Fatalf("HEAD %s: unexpected response body", path)
					}
				}
			}
			for _, path := range []string{"/tasks/task-123/edit", "/missing.js", "/missing-page", "/tasks/id/unknown", "/connections/id", "/api/missing"} {
				r := httptest.NewRecorder()
				handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
				if r.Code != http.StatusNotFound {
					t.Errorf("%s: got %d, want 404", path, r.Code)
				}
			}
			for _, path := range []string{"/app.js", "/style.css", "/toast.js", "/select.js", "/redisshake-logo.png", "/redis-logo.svg"} {
				r := httptest.NewRecorder()
				handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
				if r.Code != http.StatusOK || strings.HasPrefix(r.Header().Get("Content-Type"), "text/html") {
					t.Errorf("%s: asset did not load correctly", path)
				}
			}
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/tasks", nil))
			if r.Code != http.StatusMethodNotAllowed {
				t.Errorf("POST page: got %d, want 405", r.Code)
			}
		})
	}
}
