package main

import "testing"

func TestFrontendAssetsAreEmbedded(t *testing.T) {
	for _, name := range []string{"static/index.html", "static/app.js", "static/dialog.js", "static/style.css"} {
		data, err := assets.ReadFile(name)
		if err != nil || len(data) == 0 {
			t.Fatalf("embedded asset %s: %v", name, err)
		}
	}
}
