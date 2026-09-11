package api

import (
	"path/filepath"
	"testing"

	"github.com/eleost04/miyohub/internal/store"
)

func TestOnboardingOnlyUpdatesItsOwnerAndDoesNotConfigureServices(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, adminToken, _ := s.CreateAdmin("admin", "test-password")
	member, _ := s.CreateUser("member", "test-password", "user")
	if admin.OnboardingStatus != "pending" || member.OnboardingStatus != "pending" {
		t.Fatal("new users did not receive optional guide")
	}
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	if w := callAPI(h, "POST", "/api/v1/profile/onboarding", "", `{"status":"complete"}`); w.Code != 401 {
		t.Fatal("unauthenticated guide write accepted")
	}
	if w := callAPI(h, "POST", "/api/v1/profile/onboarding", adminToken, `{"status":"invalid"}`); w.Code != 400 {
		t.Fatal("invalid state accepted")
	}
	if w := callAPI(h, "POST", "/api/v1/profile/onboarding", adminToken, `{"status":"dismissed","user_id":"`+member.ID+`","permissions":{"exchange":true}}`); w.Code != 200 {
		t.Fatal("dismissal failed", w.Code)
	}
	current, ok := s.UserBySession(adminToken)
	if !ok || current.OnboardingStatus != "dismissed" {
		t.Fatal("dismissal was not persisted")
	}
	for _, user := range s.ListUsers() {
		if user.ID == member.ID && (user.OnboardingStatus != "pending" || user.CanExchange()) {
			t.Fatal("foreign user's guide or grants changed")
		}
	}
	if len(s.Config().Accounts) != 0 || len(s.Config().Shop.Plans) != 0 || s.CaptchaSettingsForUser(member.ID).Source != "off" {
		t.Fatal("guide auto-configured services")
	}
	if w := callAPI(h, "POST", "/api/v1/profile/onboarding", adminToken, `{"status":"complete"}`); w.Code != 200 {
		t.Fatal("completion failed")
	}
}
