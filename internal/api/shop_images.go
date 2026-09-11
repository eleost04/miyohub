package api

import (
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode"
)

// Only public raster assets on official CDN hosts may be redirected. This
// endpoint never resolves or downloads the target and is not a general proxy.
func shopImageTarget(raw string) (string, bool) {
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n\t") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Port() != "" {
		return "", false
	}
	switch u.Host {
	case "bbs-static.miyoushe.com", "upload-bbs.miyoushe.com", "upload-bbs.mihoyo.com", "webstatic.mihoyo.com", "webstatic.miyoushe.com":
	default:
		return "", false
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || path.Clean(u.Path) != u.Path || strings.ContainsAny(u.Path, "\\%") || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return "", false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".avif":
	default:
		return "", false
	}
	// Preserve official image resizing directives, but reject arbitrary redirect
	// parameters and signed URLs that could expose credentials in a public cache.
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) > 1 {
		return "", false
	}
	for key, values := range query {
		if key != "x-oss-process" || len(values) != 1 || !strings.HasPrefix(values[0], "image/") || strings.IndexFunc(values[0], func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/_,.!-", r))
		}) >= 0 {
			return "", false
		}
	}
	return u.String(), true
}

func shopImageURL(raw string) string {
	target, ok := shopImageTarget(raw)
	if !ok {
		return ""
	}
	return "/api/v1/shop/image?src=" + url.QueryEscape(target)
}

func (s *Server) shopImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["src"]) != 1 {
		writeError(w, http.StatusBadRequest, errors.New("无效的商品图片地址"))
		return
	}
	target, ok := shopImageTarget(query.Get("src"))
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("仅支持官方 CDN 的公开商品图片"))
		return
	}
	// No account-dependent content, cookies or image bytes enter this response.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Location", target)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusFound)
}
