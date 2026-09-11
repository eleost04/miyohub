package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func secureTestRequest(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-MiyoHub-Request", "1")
}

func TestInteractiveCaptchaHasAnIsolatedCSP(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, path := range []string{"/", "/api/v1/login/sms/captcha", "/captcha-frame.html"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		policy := w.Header().Get("Content-Security-Policy")
		if path == "/captcha-frame.html" {
			if !strings.Contains(policy, "sandbox allow-scripts") || strings.Contains(policy, "allow-same-origin") || !strings.Contains(policy, "frame-ancestors 'self'") {
				t.Fatal("captcha not isolated")
			}
		} else if strings.Contains(policy, "geetest") || strings.Contains(policy, "unsafe-eval") {
			t.Fatal("third-party scripts allowed in app")
		}
	}
}
func callAPI(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	secureTestRequest(r)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "miyohub_session", Value: token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestRequestProtection(t *testing.T) {
	s, _ := store.New(filepath.Join(t.TempDir(), "state.json"))
	server := NewServer(s)
	h := server.Handler()
	for _, test := range []struct {
		name, origin, header, contentType, body string
		want                                    int
	}{
		{"foreign", "https://attacker.test", "1", "application/json", `{"username":"attacker","password":"password123"}`, 403},
		{"simple form", "", "", "application/x-www-form-urlencoded", "username=a", 403},
		{"missing header", "", "", "application/json", "{}", 403},
		{"wrong type", "", "1", "text/plain", "{}", 415},
		{"multiple JSON", "", "1", "application/json", `{"username":"admin","password":"password123"} {}`, 400},
		{"too large", "", "1", "application/json", `{"username":"` + strings.Repeat("a", 1<<20) + `"}`, 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/v1/auth/setup", strings.NewReader(test.body))
			r.Header.Set("Origin", test.origin)
			r.Header.Set("X-MiyoHub-Request", test.header)
			r.Header.Set("Content-Type", test.contentType)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("reflected unsafe origin")
			}
		})
	}
	if s.HasAdmin() {
		t.Fatal("rejected request changed state")
	}
}
func TestAuthRateLimitIgnoresForwardedAddress(t *testing.T) {
	s, _ := store.New(filepath.Join(t.TempDir(), "state.json"))
	h := NewServer(s).Handler()
	for n := 0; n < 11; n++ {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"nobody","password":"incorrect"}`))
		secureTestRequest(r)
		r.Header.Set("X-Forwarded-For", string(rune('a'+n)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if n == 10 && w.Code != 429 {
			t.Fatal("rate limiter bypassed", w.Code)
		}
	}
}
func TestSecureCookieUsesConfiguredOrigin(t *testing.T) {
	for _, secure := range []bool{false, true} {
		s, _ := store.New(filepath.Join(t.TempDir(), "state.json"))
		options := Options{}
		if secure {
			options.PublicOrigin = "https://miyo.test"
		}
		h := NewServerWithOptions(s, options).Handler()
		r := httptest.NewRequest("POST", "/api/v1/auth/setup", strings.NewReader(`{"username":"admin","password":"password123"}`))
		secureTestRequest(r)
		r.Header.Set("X-Forwarded-Proto", "https")
		if secure {
			r.Header.Set("Origin", "https://miyo.test")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		c := w.Result().Cookies()[0]
		if c.Secure != secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Fatal("unexpected cookie policy")
		}
	}
}
func TestAPIOwnershipAndPendingRegistration(t *testing.T) {
	s, _ := store.New(filepath.Join(t.TempDir(), "state.json"))
	admin, _, _ := s.CreateAdmin("admin", "password123")
	u, err := s.CreateUser("member", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _ := s.Authenticate("member", "password123")
	_ = s.AddAccountForUser(admin.ID, model.Account{Name: "PRIVATE-ACCOUNT", Cookie: "PRIVATE-COOKIE", Stuid: "999"})
	other := s.AccountsForUser(admin.ID, false)[0]
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "own", Cookie: "OWN-COOKIE"})
	p, _ := s.CreateExchangePlan(admin.ID, true, model.ExchangePlan{AccountID: other.ID, GoodsID: "g", GoodsName: "PRIVATE-GOODS", Enabled: true})
	_ = s.AddLogForUser(admin.ID, "test", "PRIVATE-LOG")
	cfg := s.Config()
	cfg.Push.Channels = []model.PushChannel{{Provider: "custom", Webhook: "PRIVATE-WEBHOOK", APIURL: "PRIVATE-API"}}
	_ = s.ReplaceConfig(cfg)
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	for _, path := range []string{"/api/v1/config", "/api/v1/accounts", "/api/v1/status", "/api/v1/shop/plans"} {
		w := callAPI(h, "GET", path, token, "")
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "PRIVATE-") || strings.Contains(w.Body.String(), "OWN-COOKIE") {
			t.Fatal("cross-user or secret data leak", path)
		}
	}
	for _, test := range []struct{ method, path, body string }{
		{"GET", "/api/v1/admin/users", ""}, {"GET", "/api/v1/admin/invite-codes", ""},
		{"PUT", "/api/v1/config", `{"enabled":false}`},
		{"DELETE", "/api/v1/accounts?id=" + other.ID, ""},
		{"PUT", "/api/v1/accounts", `{"id":"` + other.ID + `","name":"stolen"}`},
		{"GET", "/api/v1/shop/points?account_id=" + other.ID, ""},
		{"GET", "/api/v1/shop/addresses?account_id=" + other.ID, ""},
		{"GET", "/api/v1/shop/roles?account_id=" + other.ID, ""},
		{"POST", "/api/v1/shop/exchange", `{"id":"` + p.ID + `","account_id":"` + s.AccountsForUser(u.ID, false)[0].ID + `"}`},
		{"POST", "/api/v1/shop/plans/cancel", `{"id":"` + p.ID + `"}`},
		{"DELETE", "/api/v1/shop/plans?id=" + p.ID, ""},
		{"POST", "/api/v1/shop/device-fp", `{"account_id":"` + other.ID + `"}`},
	} {
		w := callAPI(h, test.method, test.path, token, test.body)
		if w.Code < 400 {
			t.Fatal("foreign access allowed", test.path, w.Body.String())
		}
	}
	if got, _ := s.ExchangePlanForUser(admin.ID, true, p.ID); got.State != "pending" {
		t.Fatal("foreign exchange changed state")
	}
	pending := callAPI(h, "POST", "/api/v1/auth/register", "", `{"username":"pending","password":"password123"}`)
	if pending.Code != 200 || len(pending.Result().Cookies()) != 0 {
		t.Fatal("pending registration got session")
	}
	var result struct {
		Data struct {
			Pending bool `json:"pending"`
		}
	}
	_ = json.Unmarshal(pending.Body.Bytes(), &result)
	if !result.Data.Pending {
		t.Fatal("pending response missing")
	}
	_ = s.UpdateUserStatus(u.ID, "disabled")
	if w := callAPI(h, "GET", "/api/v1/accounts", token, ""); w.Code != 401 {
		t.Fatal("disabled user session remained active")
	}
}
func TestRateLimiterBoundsKeyCount(t *testing.T) {
	limits := rateLimits{entries: map[string]limitEntry{}}
	for n := 0; n < 4096; n++ {
		limits.entries[string(rune(n))] = limitEntry{until: time.Now().Add(time.Minute)}
	}
	if limits.allow("new", 1, time.Minute) {
		t.Fatal("unbounded limiter keys")
	}
}
