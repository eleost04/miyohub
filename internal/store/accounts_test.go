package store

import (
	"path/filepath"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestCookieRenewalCannotOverwriteNewerCredentials(t *testing.T) {
	for _, change := range []string{"cookie", "stoken", "mid", "disabled", "deleted"} {
		t.Run(change, func(t *testing.T) {
			s, err := New(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			u, _, err := s.CreateAdmin("admin", "password123")
			if err != nil {
				t.Fatal(err)
			}
			if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "stuid=100;stoken=old;mid=old-mid;cookie_token=old-token"}); err != nil {
				t.Fatal(err)
			}
			before := s.AccountsForUser(u.ID, false)[0]
			patch := AccountPatch{ID: before.ID}
			switch change {
			case "cookie":
				patch.Cookie = "stuid=100;stoken=new;mid=new-mid;cookie_token=new-token"
			case "stoken":
				patch.Stoken = "new"
			case "mid":
				patch.Mid = "new-mid"
			case "disabled":
				value := true
				patch.Disabled = &value
			}
			if change == "deleted" {
				err = s.DeleteAccount(u, before.ID)
			} else {
				_, err = s.UpdateAccount(u, patch)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.RenewAccountCookie(before, "stuid=100;cookie_token=late"); err == nil {
				t.Fatal("stale renewal overwrote newer credentials")
			}
			if change == "cookie" {
				got, _ := s.AccountForUser(u.ID, false, before.ID)
				if got.Cookie != patch.Cookie || got.Stoken != "new" || got.Mid != "new-mid" {
					t.Fatal("editing cookie did not update embedded SToken/MID")
				}
			}
		})
	}
}
