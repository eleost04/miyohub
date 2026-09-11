package api

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestInvitationAPIHasSecureDefaultsAndRejectsForgedGrants(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, adminToken, _ := s.CreateAdmin("admin", "test-password")
	_, _ = s.CreateUser("member", "test-password", "user")
	_, memberToken, _ := s.Authenticate("member", "test-password")
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	for _, token := range []string{"", memberToken} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			if w := callAPI(h, method, "/api/v1/admin/invite-codes", token, `{}`); w.Code != 401 && w.Code != 403 {
				t.Fatalf("unprivileged %s invitation access: %d", method, w.Code)
			}
		}
	}
	if w := callAPI(h, "POST", "/api/v1/admin/invite-codes", adminToken, `{"code":"predictable"}`); w.Code != 400 {
		t.Fatal("accepted manually chosen invite", w.Code)
	}
	w := callAPI(h, "POST", "/api/v1/admin/invite-codes", adminToken, `{}`)
	var reply struct {
		Data model.InviteCode `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 {
		t.Fatal("could not create default invite", w.Code, err)
	}
	c := reply.Data
	if c.MaxUses != 1 || len(c.Code) != 52 || !c.Permissions.Exchange || !c.Permissions.SiteCaptcha || !c.UseSiteCaptcha || time.Until(c.ExpiresAt) < 6*24*time.Hour || time.Until(c.ExpiresAt) > 7*24*time.Hour {
		t.Fatal("unsafe invitation defaults")
	}
	w = callAPI(h, "POST", "/api/v1/admin/invite-codes", adminToken, `{"permissions":{"exchange":false,"site_captcha":false},"use_site_captcha":false,"max_uses":1}`)
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || w.Code != 200 {
		t.Fatal("restricted invite failed")
	}
	w = callAPI(h, "POST", "/api/v1/auth/register", "", jsonBody(t, map[string]any{"username": "limited", "password": "test-password", "invite_code": reply.Data.Code, "role": "admin", "permissions": model.UserPermissions{Exchange: true, SiteCaptcha: true}, "use_site_captcha": true}))
	if w.Code != 200 {
		t.Fatal("invited registration failed", w.Code)
	}
	limited, _, err := s.Authenticate("limited", "test-password")
	if err != nil || limited.Role != "user" || limited.CanExchange() || limited.CanUseSiteCaptcha() || s.CaptchaSettingsForUser(limited.ID).Source != "off" {
		t.Fatal("registration request forged permissions", err)
	}
	w = callAPI(h, "GET", "/api/v1/bootstrap", memberToken, "")
	if strings.Contains(w.Body.String(), c.Code) {
		t.Fatal("invitation disclosed through ordinary-user bootstrap")
	}
}
