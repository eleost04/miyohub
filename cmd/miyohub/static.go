package main

import (
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var hashedAsset = regexp.MustCompile(`^assets/[A-Za-z0-9_.-]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

func staticHandler(apiHandler http.Handler) http.Handler {
	return staticHandlerAt(apiHandler, "web/dist")
}

func staticHandlerAt(apiHandler http.Handler, directory string) http.Handler {
	dist := os.DirFS(directory)
	if _, err := fs.Stat(dist, "."); err != nil {
		return apiHandler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			// Never compress or cache account data together with public assets.
			apiHandler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || name == "register" {
			name = "index.html"
		}
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			http.NotFound(w, r)
			return
		}
		original, err := fs.Stat(dist, name)
		if err != nil || !original.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		fileName, encoding, representationSize := name, "", original.Size()
		switch path.Ext(name) {
		case ".js", ".css", ".html", ".svg":
			w.Header().Add("Vary", "Accept-Encoding")
			for _, candidate := range preferredEncodings(r.Header.Get("Accept-Encoding")) {
				suffix := "." + candidate
				if candidate == "gzip" {
					suffix = ".gz"
				}
				if info, err := fs.Stat(dist, name+suffix); err == nil && info.Mode().IsRegular() && info.Size() < original.Size() && !info.ModTime().Before(original.ModTime()) {
					fileName, encoding, representationSize = name+suffix, candidate, info.Size()
					break
				}
			}
		}
		file, err := dist.Open(fileName)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		content, ok := file.(io.ReadSeeker)
		if !ok {
			http.Error(w, "无法读取静态资源", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if hashedAsset.MatchString(name) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%x-%x-%s"`, original.ModTime().UnixNano(), original.Size(), encoding))
		if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if encoding != "" {
			w.Header().Set("Content-Encoding", encoding)
		}
		w.Header().Set("Content-Length", strconv.FormatInt(representationSize, 10))
		http.ServeContent(w, r, name, original.ModTime(), content)
	})
}

// Honor explicit exclusions (gzip;q=0) even if a wildcard is also accepted.
func preferredEncodings(header string) []string {
	values := map[string]float64{}
	for _, part := range strings.Split(header, ",") {
		parts := strings.Split(part, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		q := 1.0
		for _, parameter := range parts[1:] {
			key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if found && strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil || parsed < 0 || parsed > 1 || parsed != parsed {
					q = 0
				} else {
					q = parsed
				}
			}
		}
		values[name] = q
	}
	quality := func(name string) float64 {
		if value, exists := values[name]; exists {
			return value
		}
		return values["*"]
	}
	names := []string{"br", "gzip"}
	if quality("gzip") > quality("br") {
		names[0], names[1] = names[1], names[0]
	}
	result := []string{}
	for _, name := range names {
		if quality(name) > 0 {
			result = append(result, name)
		}
	}
	return result
}
