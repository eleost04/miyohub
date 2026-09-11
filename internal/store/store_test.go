package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestAdminLifecycleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := s.Config(); cfg.Schedule.Timezone != "Asia/Shanghai" || cfg.Accounts == nil || cfg.Shop.Plans == nil {
		t.Fatalf("unexpected default config shape: %#v", cfg.Schedule)
	}
	user, token, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != "admin" || token == "" {
		t.Fatalf("unexpected admin: %#v", user)
	}
	authenticated, _, err := s.Authenticate("admin", "password123")
	if err != nil || authenticated.ID != user.ID {
		t.Fatalf("authenticate: %#v %v", authenticated, err)
	}
	if _, ok := s.UserBySession(token); !ok {
		t.Fatal("session should be valid")
	}
	if _, err := New(filepath.Join(t.TempDir(), "state.json")); err != nil {
		t.Fatal(err)
	}
}

func TestAccountsAreIsolatedByUser(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := s.Register("user", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserStatus(user.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(admin.ID, model.Account{Name: "admin-account", Cookie: "admin-cookie"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(user.ID, model.Account{Name: "user-account", Cookie: "user-cookie"}); err != nil {
		t.Fatal(err)
	}
	adminAccounts := s.AccountsForUser(admin.ID, false)
	userAccounts := s.AccountsForUser(user.ID, false)
	if len(adminAccounts) != 1 || adminAccounts[0].Name != "admin-account" {
		t.Fatalf("unexpected admin accounts: %#v", adminAccounts)
	}
	if len(userAccounts) != 1 || userAccounts[0].Name != "user-account" {
		t.Fatalf("unexpected user accounts: %#v", userAccounts)
	}
	if _, ok := s.AccountForUser(user.ID, false, adminAccounts[0].ID); ok {
		t.Fatal("user should not access another user's account")
	}
	if all := s.AccountsForUser(user.ID, true); len(all) != 1 {
		t.Fatal("admin flag bypassed owner isolation")
	}
	if all := s.AccountsForUser("", true); len(all) != 2 {
		t.Fatal("internal scheduler cannot enumerate accounts")
	}
}

func TestInviteRegistrationAndAdminControls(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	pending, token, err := s.RegisterWithInvite("pending", "password123", "")
	if err != nil || token != "" || pending.Status != "pending" {
		t.Fatalf("expected pending registration, user=%#v token=%q err=%v", pending, token, err)
	}
	if err := s.UpdateUserStatus(pending.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate("pending", "password123"); err != nil {
		t.Fatalf("activated user should authenticate: %v", err)
	}
	invite, err := s.CreateInviteCode("launch", time.Now().Add(time.Hour), 1, "test", admin.ID)
	if err != nil || invite.Code != "LAUNCH" {
		t.Fatalf("unexpected invite: %#v err=%v", invite, err)
	}
	active, token, err := s.RegisterWithInvite("invited", "password123", "launch")
	if err != nil || token == "" || active.Status != "active" {
		t.Fatalf("expected active invite registration, user=%#v token=%q err=%v", active, token, err)
	}
	if _, _, err := s.RegisterWithInvite("second", "password123", "launch"); err == nil {
		t.Fatal("invite should be exhausted after one use")
	}
	if err := s.UpdateUserRole(active.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserRole(admin.ID, "user"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserRole(active.ID, "user"); err == nil {
		t.Fatal("should not demote the last active administrator")
	}
	if err := s.DeleteInviteCode("launch"); err != nil {
		t.Fatal(err)
	}
}

func TestExchangePlansAreScopedToAccountOwner(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := s.RegisterWithInvite("user", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserStatus(user.ID, "active"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(user.ID, model.Account{Name: "owned"}); err != nil {
		t.Fatal(err)
	}
	account := s.AccountsForUser(user.ID, false)[0]
	if err := s.UpdateUserPermissions(admin, user.ID, model.UserPermissions{Exchange: true}); err != nil {
		t.Fatal(err)
	}
	plan, err := s.CreateExchangePlan(user.ID, false, model.ExchangePlan{GoodsID: "goods", AccountID: account.ID, Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ExchangePlanForUser(admin.ID, false, plan.ID); ok {
		t.Fatal("other user should not see exchange plan")
	}
	if _, ok := s.ExchangePlanForUser(admin.ID, true, plan.ID); ok {
		t.Fatal("admin should not see another user's exchange plan")
	}
	if err := s.DeleteExchangePlan(admin.ID, false, plan.ID); err == nil {
		t.Fatal("other user should not delete exchange plan")
	}
}

func TestReplaceConfigPreservesRedactedSecrets(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccount(model.Account{Name: "main", Cookie: "cookie", Stuid: "1001", Stoken: "stoken", Mid: "mid", CloudTokens: map[string]string{"genshin": "cloud"}}); err != nil {
		t.Fatal(err)
	}
	cfg := s.Config()
	cfg.Push.Channels = []model.PushChannel{{Provider: "test", Token: "token", Secret: "secret", ClientSecret: "client"}}
	cfg.Captcha.Channels = []model.CaptchaChannel{{Provider: "damagou", UserKey: "key"}}
	if err := s.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	public := s.Config()
	public.Accounts[0].Cookie = ""
	public.Accounts[0].Stuid = ""
	public.Accounts[0].Stoken = ""
	public.Accounts[0].Mid = ""
	public.Accounts[0].CloudTokens = nil
	public.Push.Channels[0].Token = ""
	public.Push.Channels[0].Secret = ""
	public.Push.Channels[0].ClientSecret = ""
	public.Captcha.Channels[0].UserKey = ""
	if err := s.ReplaceConfig(public); err != nil {
		t.Fatal(err)
	}
	stored := s.Config()
	account := stored.Accounts[0]
	if account.Cookie != "cookie" || account.Stoken != "stoken" || account.CloudTokens["genshin"] != "cloud" {
		t.Fatalf("account secrets not preserved: %#v", account)
	}
	if stored.Push.Channels[0].Token != "token" || stored.Push.Channels[0].Secret != "secret" || stored.Captcha.Channels[0].UserKey != "key" {
		t.Fatalf("config secrets not preserved: %#v %#v", stored.Push, stored.Captcha)
	}
}

func TestRebindingKeepsAccountAndPlans(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("admin", "password123")
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "stuid=100;stoken=old", CloudTokens: map[string]string{"genshin": "cloud"}}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	if _, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "gift"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "new", Cookie: "stuid=100;stoken=new"}); err != nil {
		t.Fatal(err)
	}
	next := s.AccountsForUser(u.ID, false)[0]
	if next.ID != a.ID || next.Stoken != "new" || next.CloudTokens["genshin"] != "cloud" || len(s.ExchangePlansForUser(u.ID, false)) != 1 {
		t.Fatal("rebind changed identity or lost dependent records")
	}
	other, _, _ := s.Register("other", "password123")
	s.UpdateUserStatus(other.ID, "active")
	if err := s.AddAccountForUser(other.ID, model.Account{Name: "steal", Cookie: "stuid=100;stoken=token"}); err == nil {
		t.Fatal("same UID claimed by another user")
	}
	if err := s.DeleteAccount(model.User{ID: other.ID, Role: "user", Status: "active"}, a.ID); err == nil {
		t.Fatal("cross user delete allowed")
	}
	if err := s.DeleteAccount(u, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.ExchangePlansForUser(u.ID, true)) != 0 {
		t.Fatal("delete did not cascade plans")
	}
}

func TestRegistrationReviewRevocationAndUserCleanup(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "state.json"))
	if _, _, err := s.Register("early", "password123"); err == nil {
		t.Fatal("registration bypassed setup")
	}
	_, _, _ = s.CreateAdmin("admin", "password123")
	pending, token, err := s.Register("pending", "password123")
	if err != nil || token != "" || pending.Status != "pending" || pending.CreatedAt.IsZero() {
		t.Fatal("invalid pending registration", err)
	}
	if _, _, err := s.Authenticate("pending", "password123"); err == nil {
		t.Fatal("pending user signed in")
	}
	_ = s.UpdateUserStatus(pending.ID, "active")
	_, token, err = s.Authenticate("pending", "password123")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.ResetUserPassword(pending.ID, "newpassword123")
	if _, ok := s.UserBySession(token); ok {
		t.Fatal("reset kept old session")
	}
	_ = s.AddAccountForUser(pending.ID, model.Account{Name: "a"})
	a := s.AccountsForUser(pending.ID, false)[0]
	_, _ = s.CreateExchangePlan(pending.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g"})
	_ = s.AddLogForUser(pending.ID, "test", "private")
	if err := s.DeleteUser(pending.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Config().Accounts) != 0 || len(s.Config().Shop.Plans) != 0 || len(s.LogsForUser(pending.ID, false)) != 0 {
		t.Fatal("orphaned user data")
	}
	_ = s.AddLogForUser(pending.ID, "test", "late")
	if len(s.LogsForUser(pending.ID, false)) != 0 {
		t.Fatal("late task recreated deleted user logs")
	}
}
func TestInviteValidationAndFailedRegistrationDoesNotConsume(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "state.json"))
	admin, _, _ := s.CreateAdmin("admin", "password123")
	if _, err := s.CreateInviteCode("bad/code", time.Time{}, 1, "", admin.ID); err == nil {
		t.Fatal("unsafe URL code")
	}
	_, err := s.CreateInviteCode("GOOD", time.Time{}, 1, "", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RegisterWithInvite("invalid user", "password123", "GOOD"); err == nil {
		t.Fatal("invalid username accepted")
	}
	if s.ListInviteCodes()[0].UsedCount != 0 {
		t.Fatal("invalid registration consumed invite")
	}
	_ = s.DisableInvite("GOOD", true)
	if _, _, err := s.RegisterWithInvite("invited", "password123", "GOOD"); err == nil {
		t.Fatal("revoked invite accepted")
	}
	_ = s.DisableInvite("GOOD", false)
	u, token, err := s.RegisterWithInvite("invited", "password123", "GOOD")
	if err != nil || token == "" || u.Status != "active" {
		t.Fatal("invite registration failed", err)
	}
}
