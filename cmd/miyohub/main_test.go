package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticRootAndInviteRoute(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("web/dist", 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("web", "dist", "index.html"), []byte("app-entry"), 0600); err != nil {
		t.Fatal(err)
	}
	h := staticHandler(http.NotFoundHandler())
	for _, path := range []string{"/", "/?invite=CODE", "/register?invite=CODE"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "app-entry") {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
