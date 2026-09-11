package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func inviteFixture(t *testing.T) (*Store, model.User) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, err := s.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	return s, admin
}

func TestRandomInvitationGrantsOnlyItsConfiguredPermissions(t *testing.T) {
	s, admin := inviteFixture(t)
	grant := InviteGrant{Permissions: model.UserPermissions{Exchange: true, SiteCaptcha: true}, UseSiteCaptcha: true}
	first, err := s.CreateInviteCode("", time.Now().Add(time.Hour), 1, "fixture", admin.ID, grant)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateInviteCode("", time.Now().Add(time.Hour), 1, "", admin.ID, InviteGrant{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Code) != 52 || !invitePattern.MatchString(first.Code) || first.Code == second.Code {
		t.Fatal("invitation entropy/format is incorrect")
	}
	u, token, err := s.RegisterWithInvite("invited", "test-password", strings.ToLower(first.Code))
	if err != nil || token == "" || u.Role != "user" || !u.CanExchange() || !u.CanUseSiteCaptcha() || u.ID == "" || s.CaptchaSettingsForUser(u.ID).Source != "site" {
		t.Fatal("grant not inherited correctly", err)
	}
	limited, _, err := s.RegisterWithInvite("limited", "test-password", second.Code)
	if err != nil || limited.CanExchange() || limited.CanUseSiteCaptcha() || s.CaptchaSettingsForUser(limited.ID).Source != "off" {
		t.Fatal("ungranted permission inherited", err)
	}
	if err := s.SetRegistrationMode("open"); err != nil {
		t.Fatal(err)
	}
	open, _, err := s.Register("ordinary", "test-password")
	if err != nil || open.CanExchange() || open.CanUseSiteCaptcha() {
		t.Fatal("open registration acquired invitation privileges", err)
	}
}

func TestInvitationLastUseIsAtomic(t *testing.T) {
	s, admin := inviteFixture(t)
	code, err := s.CreateInviteCode("", time.Now().Add(time.Hour), 1, "", admin.ID, InviteGrant{Permissions: model.UserPermissions{Exchange: true}})
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, _, err := s.RegisterWithInvite(fmt.Sprintf("member%d", n), "test-password", code.Code); err == nil {
				successes.Add(1)
			}
		}(n)
	}
	wg.Wait()
	if successes.Load() != 1 || len(s.ListUsers()) != 2 || s.ListInviteCodes()[0].UsedCount != 1 {
		t.Fatal("single-use invite raced", successes.Load())
	}
}

func TestInvalidInvitesAndRevokedCreatorsCannotGrantAccess(t *testing.T) {
	for _, state := range []string{"expired", "disabled", "creator-disabled", "creator-demoted", "creator-deleted"} {
		t.Run(state, func(t *testing.T) {
			s, admin := inviteFixture(t)
			if _, err := s.CreateUser("backup", "test-password", "admin"); err != nil {
				t.Fatal(err)
			}
			code, err := s.CreateInviteCode("", time.Now().Add(time.Hour), 1, "", admin.ID, InviteGrant{Permissions: model.UserPermissions{Exchange: true}})
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "expired":
				s.data.InviteCodes[0].ExpiresAt = time.Now().Add(-time.Second)
			case "disabled":
				err = s.DisableInvite(code.Code, true)
			case "creator-disabled":
				err = s.UpdateUserStatus(admin.ID, "disabled")
			case "creator-demoted":
				err = s.UpdateUserRole(admin.ID, "user")
			case "creator-deleted":
				err = s.DeleteUser(admin.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, token, err := s.RegisterWithInvite("intruder", "test-password", code.Code); err == nil || token != "" || err.Error() != "邀请码无效、已过期或已用尽" {
				t.Fatal("invalid invite accepted or disclosed internal state", err)
			}
		})
	}
	s, admin := inviteFixture(t)
	user, _ := s.CreateUser("member", "test-password", "user")
	if _, err := s.CreateInviteCode("", time.Time{}, 1, "", user.ID); err == nil {
		t.Fatal("ordinary user minted invitation")
	}
	if _, err := s.CreateInviteCode("", time.Time{}, 1, "", admin.ID, InviteGrant{UseSiteCaptcha: true}); err == nil {
		t.Fatal("site default enabled without permission")
	}
}

func TestInvitationSaveFailuresRollbackUserSessionAndConsumption(t *testing.T) {
	s, admin := inviteFixture(t)
	code, err := s.CreateInviteCode("", time.Now().Add(time.Hour), 1, "", admin.ID, InviteGrant{Permissions: model.UserPermissions{SiteCaptcha: true}, UseSiteCaptcha: true})
	if err != nil {
		t.Fatal(err)
	}
	users, sessions := len(s.data.Users), len(s.data.Sessions)
	s.path = t.TempDir() // A directory cannot be atomically replaced by the state file.
	if _, _, err := s.RegisterWithInvite("unsaved", "test-password", code.Code); err == nil {
		t.Fatal("injected write failure succeeded")
	}
	if len(s.data.Users) != users || len(s.data.Sessions) != sessions || len(s.data.UserCaptcha) != 0 || s.ListInviteCodes()[0].UsedCount != 0 {
		t.Fatal("partial registration survived a save failure")
	}
	if _, err := s.CreateInviteCode("", time.Time{}, 1, "", admin.ID); err == nil {
		t.Fatal("failed invite creation succeeded")
	}
	if codes := s.ListInviteCodes(); len(codes) != 1 || codes[0].Code != code.Code {
		t.Fatal("failed creation removed a previously committed invite")
	}
}
