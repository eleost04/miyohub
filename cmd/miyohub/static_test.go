package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPreferredStaticEncodings(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   []string
	}{
		{"", []string{}}, {"identity", []string{}},
		{"gzip, deflate, br", []string{"br", "gzip"}},
		{"br;q=0.3, gzip;q=1", []string{"gzip", "br"}},
		{"gzip;q=0, br;q=0, *;q=1", []string{}},
		{"gzip;q=0, *;q=0.5", []string{"br"}},
		{"BR;Q=1, gzip;q=0", []string{"br"}},
		{"br;q=invalid, gzip;q=2", []string{}},
		{"br;q=NaN, gzip;q=-1", []string{}},
	} {
		if got := preferredEncodings(tc.header); !reflect.DeepEqual(got, tc.want) {
			t.Fatal("wrong encoding negotiation", tc.header, got)
		}
	}
}

func TestStaticCompressionCacheAndAPISeparation(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0750); err != nil {
		t.Fatal(err)
	}
	asset := "assets/index-123456ab.js"
	raw := []byte(strings.Repeat("const text = 'static asset';\n", 128))
	var zipped bytes.Buffer
	encoder := gzip.NewWriter(&zipped)
	_, _ = encoder.Write(raw)
	_ = encoder.Close()
	// Serving does not parse Brotli. Build/browser tests verify its actual bytes.
	bro := []byte("prebuilt-brotli-representation")
	stamp := time.Unix(1700000000, 0)
	for name, content := range map[string][]byte{asset: raw, asset + ".gz": zipped.Bytes(), asset + ".br": bro, "index.html": []byte("app-entry")} {
		filename := filepath.Join(directory, name)
		if err := os.WriteFile(filename, content, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filename, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	h := staticHandlerAt(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("private API response"))
	}), directory)
	request := func(method, path, encoding, etag string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Accept-Encoding", encoding)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	plain := request("GET", "/"+asset, "identity", "")
	if plain.Code != 200 || !bytes.Equal(plain.Body.Bytes(), raw) || plain.Header().Get("Content-Encoding") != "" || plain.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || plain.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatal("incorrect plain asset or cache policy", plain.Code, plain.Header())
	}
	compressed := request("GET", "/"+asset, "gzip", "")
	decoder, err := gzip.NewReader(compressed.Body)
	if err != nil {
		t.Fatal("invalid compressed representation", err)
	}
	decoded, _ := io.ReadAll(decoder)
	_ = decoder.Close()
	if !bytes.Equal(decoded, raw) || compressed.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(compressed.Header().Get("Content-Type"), "javascript") || compressed.Header().Get("ETag") == plain.Header().Get("ETag") {
		t.Fatal("encoded representation changed bytes, MIME or validator")
	}
	brotli := request("GET", "/"+asset, "gzip, br", "")
	if brotli.Code != 200 || brotli.Header().Get("Content-Encoding") != "br" || !bytes.Equal(brotli.Body.Bytes(), bro) {
		t.Fatal("Brotli was not served")
	}
	for _, method := range []string{"GET", "HEAD"} {
		cached := request(method, "/"+asset, "gzip", compressed.Header().Get("ETag"))
		if cached.Code != 304 || cached.Body.Len() != 0 {
			t.Fatal("conditional request downloaded unchanged asset", method, cached.Code)
		}
	}
	head := request("HEAD", "/"+asset, "gzip", "")
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(zipped.Len()) {
		t.Fatal("encoded HEAD changed content length or wrote a body")
	}
	for _, path := range []string{"/", "/register?invite=TEST", "/index.html"} {
		entry := request("GET", path, "gzip, br", "")
		if entry.Code != 200 || entry.Header().Get("Cache-Control") != "no-cache" || entry.Body.String() != "app-entry" {
			t.Fatal("entry document can outlive a deployment", path, entry.Code)
		}
	}
	for _, path := range []string{"/assets/missing-123456ab.js", "/assets/", "/../state.json"} {
		res := request("GET", path, "gzip, br", "")
		if res.Code != 404 || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing assets or directories were exposed/cached", path, res.Code)
		}
	}
	private := request("GET", "/api/v1/bootstrap", "gzip, br", "")
	if private.Code != 200 || private.Header().Get("Cache-Control") != "no-store" || private.Header().Get("Content-Encoding") != "" || private.Body.String() != "private API response" {
		t.Fatal("private API entered public static handling")
	}
	if write := request("POST", "/"+asset, "gzip", ""); write.Code != 405 || write.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("static endpoint accepted a write")
	}
	if err := os.Chtimes(filepath.Join(directory, asset+".gz"), stamp.Add(-time.Second), stamp.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	stale := request("GET", "/"+asset, "gzip", "")
	if stale.Header().Get("Content-Encoding") != "" || !bytes.Equal(stale.Body.Bytes(), raw) {
		t.Fatal("stale compressed sidecar overrode newer source")
	}
}
