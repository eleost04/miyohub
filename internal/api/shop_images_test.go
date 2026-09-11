package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/store"
)

func TestShopImageTargetAllowlist(t *testing.T) {
	for _, valid := range []string{
		"https://bbs-static.miyoushe.com/static/2026/09/test.JPG",
		"https://upload-bbs.miyoushe.com/upload/2026/test.png",
		"https://upload-bbs.mihoyo.com/upload/test.webp",
		"https://webstatic.mihoyo.com/upload/test.avif",
		"https://webstatic.miyoushe.com/upload/test.png?x-oss-process=image%2Fresize,w_256",
	} {
		if target, ok := shopImageTarget(valid); !ok || target != valid {
			t.Fatal("valid CDN image rejected", valid)
		}
	}
	for _, invalid := range []string{
		"", "//bbs-static.miyoushe.com/static/test.png", "http://bbs-static.miyoushe.com/static/test.png",
		"https://127.0.0.1/test.png", "https://169.254.169.254/test.png", "https://example.com/test.png",
		"https://bbs-static.miyoushe.com.evil.invalid/test.png", "https://bbs-static.miyoushe.com@evil.invalid/test.png",
		"https://secret@bbs-static.miyoushe.com/static/test.png", "https://bbs-static.miyoushe.com:443/static/test.png",
		"https://bbs-static.miyoushe.com/static/test.svg", "https://bbs-static.miyoushe.com/redirect?url=https://example.com/test.png",
		"https://bbs-static.miyoushe.com/static/test.png?redirect=https://evil.invalid", "https://bbs-static.miyoushe.com/static/test.png?token=secret",
		"https://bbs-static.miyoushe.com/static/test.png?x-oss-process=image/resize,w_256|sys/saveas",
		"https://bbs-static.miyoushe.com/static/test.png?x-oss-process=image/resize&w=1",
		"https://bbs-static.miyoushe.com/static/test.png?x-oss-process=image/resize&x-oss-process=image/resize",
		"https://bbs-static.miyoushe.com/static/test.png#secret", "https://bbs-static.miyoushe.com/static/../test.png",
		"https://bbs-static.miyoushe.com//evil.invalid/test.png", "https://bbs-static.miyoushe.com/static/%0d%0atest.png",
		"https://bbs-static.miyoushe.com/static/%252etest.png", "https://bbs-static.miyoushe.com/static\\test.png",
	} {
		if _, ok := shopImageTarget(invalid); ok || shopImageURL(invalid) != "" {
			t.Fatal("unsafe target accepted", invalid)
		}
	}
}

func TestShopImageRedirectIsPublicCachedAndDoesNotFetch(t *testing.T) {
	state, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, _ := state.CreateAdmin("admin", "test-password")
	server := NewServer(state)
	defer server.Stop()
	server.shopClient.HTTP.Transport = probeTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("image redirect attempted an upstream download")
		return nil, nil
	})
	h := server.Handler()
	target := "https://bbs-static.miyoushe.com/static/2026/test.png"
	endpoint := shopImageURL(target)
	for i := 0; i < 40; i++ {
		res := callAPI(h, http.MethodGet, endpoint, "", "")
		if res.Code != 302 || res.Header().Get("Location") != target || res.Header().Get("Cache-Control") != "public, max-age=86400" || res.Body.Len() != 0 || res.Header().Get("Set-Cookie") != "" {
			t.Fatal("image not a public cacheable bodyless redirect", res.Code, res.Header())
		}
	}
	if res := callAPI(h, http.MethodHead, endpoint, "", ""); res.Code != 302 || res.Body.Len() != 0 {
		t.Fatal("HEAD should redirect without a body")
	}
	if res := callAPI(h, http.MethodPost, "/api/v1/captcha/test", token, `{}`); res.Code != 400 {
		t.Fatal("images consumed the task action quota", res.Code)
	}
	for _, path := range []string{
		"/api/v1/shop/image?src=" + url.QueryEscape("https://evil.invalid/test.png"),
		endpoint + "&src=" + url.QueryEscape(target), endpoint + "&unused=1", "/api/v1/shop/image",
	} {
		res := callAPI(h, http.MethodGet, path, "", "")
		if res.Code != 400 || res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Location") != "" {
			t.Fatal("invalid image request was cached or redirected", path, res.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, endpoint, nil)
	request.Header.Set("Origin", "https://evil.invalid")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 403 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("image bypassed same-origin protection")
	}
}

func TestShopAPIsOnlyRewritePublicImageFields(t *testing.T) {
	state, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, token, _ := state.CreateAdmin("admin", "test-password")
	server := NewServer(state)
	defer server.Stop()
	target := "https://bbs-static.miyoushe.com/static/2026/test.png"
	good := map[string]any{"goods_id": "g1", "icon": target, "price": 50, "total": 5, "status": "online"}
	server.shopClient.HTTP.Transport = probeTransport(func(r *http.Request) (*http.Response, error) {
		var data any = good
		if r.URL.Path == mihoyo.MallGoodsPath {
			data = map[string]any{"list": []any{good}, "has_more": false}
		} else if r.URL.Path != mihoyo.MallDetailPath {
			t.Fatal("unexpected upstream endpoint", r.URL.Path)
		}
		body, _ := json.Marshal(map[string]any{"retcode": 0, "data": data})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	for _, endpoint := range []string{"/api/v1/shop/goods", "/api/v1/shop/good-detail?goods_id=g1"} {
		res := callAPI(server.Handler(), http.MethodGet, endpoint, token, "")
		var body struct{ Data json.RawMessage }
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("catalog response failed or cached stock", res.Code, err)
		}
		var parsed map[string]any
		_ = json.Unmarshal(body.Data, &parsed)
		if items, ok := parsed["goods"].([]any); ok {
			parsed = items[0].(map[string]any)
		}
		if parsed["icon"] != shopImageURL(target) || parsed["price"] != float64(50) || parsed["total"] != float64(5) {
			t.Fatal("image rewriting changed catalog content", parsed)
		}
	}
}
