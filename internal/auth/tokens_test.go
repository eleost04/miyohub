package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestRefreshCookieReplacesOnlyCookieToken(t *testing.T) {
	client := mihoyo.NewClient("")
	client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/auth/api/getCookieAccountInfoBySToken" || r.Header.Get("Cookie") != "stuid=100;stoken=st;mid=mid" {
			t.Error("incorrect renewal request")
		}
		return authReply(`{"retcode":0,"data":{"cookie_token":"new-cookie-token"}}`), nil
	})
	account := model.Account{Stuid: "100", Stoken: "st", Mid: "mid", Cookie: "ltuid=100;ltoken=lt;account_id=100;cookie_token=old;cookie_token_v2=older;stuid=100;stoken=st;mid=mid"}
	cookie, err := RefreshCookie(context.Background(), client, account)
	if err != nil || strings.Contains(cookie, "old") || strings.Count(cookie, "cookie_token=") != 1 || !strings.Contains(cookie, "ltoken=lt") || !strings.Contains(cookie, "cookie_token=new-cookie-token") {
		t.Fatal("renewal changed unrelated credentials", cookie, err)
	}
	for _, body := range []string{`{"retcode":-100}`, `{"retcode":0,"data":{}}`, `{"retcode":0,"data":{"cookie_token":"token;stuid=200"}}`} {
		client.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) { return authReply(body), nil })
		if _, err := RefreshCookie(context.Background(), client, account); err == nil {
			t.Fatal("invalid renewal accepted")
		}
	}
}

func TestQRRebindingValidatesOwnerAndOriginalUIDAtCompletion(t *testing.T) {
	for _, scenario := range []string{"own", "different UID", "foreign", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			sms, u := smsFixture(t)
			s := sms.store
			owner := u
			if scenario == "foreign" {
				var err error
				owner, err = s.CreateUser("member", "password123", "user")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := s.AddAccountForUser(owner.ID, model.Account{Name: "原有名称", Stuid: "100", Cookie: "cookie_token=before", CloudTokens: map[string]string{"genshin": "cloud"}}); err != nil {
				t.Fatal(err)
			}
			a := s.AccountsForUser(owner.ID, false)[0]
			if scenario == "deleted" {
				if err := s.DeleteAccount(owner, a.ID); err != nil {
					t.Fatal(err)
				}
			}
			m := NewQRManager(s)
			defer m.Stop()
			current := &qrSession{state: model.LoginState{Running: true, AccountID: a.ID}}
			m.sessions[u.ID] = current
			m.client.HTTP.Transport = transport(func(*http.Request) (*http.Response, error) {
				return authReply(`{"retcode":0,"data":{"ltoken":"lt","cookie_token":"ct"}}`), nil
			})
			uid := "100"
			if scenario == "different UID" {
				uid = "200"
			}
			data := map[string]any{"user_info": map[string]any{"aid": uid, "mid": "mid"}, "tokens": []any{map[string]any{"token": "st"}}}
			err := m.complete(context.Background(), u.ID, current, model.Config{}, "changed", data)
			if scenario == "own" {
				got, ok := s.AccountForUser(u.ID, false, a.ID)
				if err != nil || !ok || got.Name != "原有名称" || got.Stoken != "st" || got.CloudTokens["genshin"] != "cloud" {
					t.Fatal("rebind did not preserve identity/settings", err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe rebind accepted")
				}
				if got, ok := s.AccountForUser(owner.ID, false, a.ID); ok && got.Cookie != a.Cookie {
					t.Fatal("rejected binding changed credentials")
				}
				if scenario == "deleted" && len(s.AccountsForUser(u.ID, false)) != 0 {
					t.Fatal("late login resurrected a deleted account")
				}
			}
		})
	}
}
