package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Version       string
	PublicOrigin  string
	SecureCookies bool
}

func (o Options) validate() error {
	if o.PublicOrigin == "" {
		return nil
	}
	u, err := url.Parse(o.PublicOrigin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("MIYOHUB_PUBLIC_ORIGIN 须为完整的 http(s) 站点地址，不含路径或参数")
	}
	return nil
}
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if r.URL.Path == "/captcha-frame.html" {
			// Only this opaque sandbox may load the official interactive widget.
			// Keep third-party scripts and eval forbidden on every application/API page.
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-eval' https://*.geetest.com https://*.geevisit.com https://*.geetest.cn; style-src 'self' 'unsafe-inline' https://*.geetest.com https://*.geevisit.com https://*.geetest.cn; img-src data: https://*.geetest.com https://*.geevisit.com https://*.geetest.cn; connect-src https://*.geetest.com https://*.geevisit.com https://*.geetest.cn; font-src https://*.geetest.com https://*.geevisit.com https://*.geetest.cn; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; form-action 'none'; sandbox allow-scripts")
		}
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) secureCookies(r *http.Request) bool {
	return r.TLS != nil || s.options.SecureCookies || strings.HasPrefix(s.options.PublicOrigin, "https://")
}
func (s *Server) origin(r *http.Request) string {
	if s.options.PublicOrigin != "" {
		return strings.TrimSuffix(s.options.PublicOrigin, "/")
	}
	scheme := "http"
	if s.secureCookies(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
func (s *Server) security(next http.Handler) http.Handler {
	return SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		origin := r.Header.Get("Origin")
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (origin != "" && !strings.EqualFold(origin, s.origin(r))) {
			writeError(w, 403, errors.New("不允许跨站请求"))
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead
		if unsafe {
			if r.Header.Get("X-MiyoHub-Request") != "1" {
				writeError(w, 403, errors.New("缺少请求校验头"))
				return
			}
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				writeError(w, 415, errors.New("请求必须使用 application/json"))
				return
			}
			if origin == "" && r.Referer() != "" {
				ref, err := url.Parse(r.Referer())
				if err != nil || !strings.EqualFold(ref.Scheme+"://"+ref.Host, s.origin(r)) {
					writeError(w, 403, errors.New("不允许跨站请求"))
					return
				}
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		// Proxy headers cannot choose a different rate-limit identity.
		if !s.limits.allow("ip:"+host, 600, time.Minute) {
			rateError(w)
			return
		}
		path := r.URL.Path
		if path == "/api/v1/shop/image" {
			// Cached public redirects must not consume the account action quota.
			if !s.limits.allow("images:"+host, 300, time.Minute) {
				rateError(w)
				return
			}
		} else if path == "/api/v1/auth/login" || path == "/api/v1/auth/register" || path == "/api/v1/auth/setup" {
			limit := 10
			if path != "/api/v1/auth/login" {
				limit = 5
			}
			if !s.limits.allow("auth:"+host+path, limit, time.Minute) || !s.limits.allow("auth-global", 100, time.Minute) {
				rateError(w)
				return
			}
		} else {
			identity := host
			if token := sessionToken(r); token != "" {
				sum := sha256.Sum256([]byte(token))
				identity = hex.EncodeToString(sum[:])
			}
			if !s.limits.allow("session:"+identity, 240, time.Minute) {
				rateError(w)
				return
			}
			expensive := unsafe || strings.HasPrefix(path, "/api/v1/shop/") && path != "/api/v1/shop/plans" && path != "/api/v1/shop/status"
			if expensive && !s.limits.allow("actions:"+identity, 30, time.Minute) {
				rateError(w)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	}))
}
func rateError(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	writeError(w, 429, errors.New("请求过于频繁，请稍后重试"))
}

type limitEntry struct {
	count int
	until time.Time
}
type rateLimits struct {
	mu      sync.Mutex
	entries map[string]limitEntry
}

func (l *rateLimits) allow(key string, limit int, period time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.entries == nil {
		l.entries = map[string]limitEntry{}
	}
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= 4096 {
		for k, e := range l.entries {
			if !e.until.After(now) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 4096 {
			return false
		}
	}
	if !entry.until.After(now) {
		entry = limitEntry{until: now.Add(period)}
	}
	if entry.count >= limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}
func decodeRequestError(err error) error {
	var large *http.MaxBytesError
	if errors.As(err, &large) {
		return errors.New("请求内容超过 1 MiB")
	}
	if errors.Is(err, io.EOF) {
		return errors.New("请求内容不能为空")
	}
	return fmt.Errorf("JSON 请求格式无效")
}
