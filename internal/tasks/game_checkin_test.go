package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestGameCheckinSkipsBlockedFirstBindAndSignedRoles(t *testing.T) {
	signCalls, infoCalls := 0, 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case mihoyo.AccountRolesPath:
			return reply(`{"retcode":0,"data":{"list":[{"game_uid":"blocked"},{"game_uid":"first"},{"game_uid":"signed"},{"game_uid":"ready","region":"cn"}]}}`), nil
		case "/event/luna/home":
			return reply(`{"retcode":-1}`), nil
		case "/event/luna/info":
			infoCalls++
			switch r.URL.Query().Get("uid") {
			case "blocked":
				t.Fatal("blacklisted role queried")
			case "first":
				return reply(`{"retcode":0,"data":{"first_bind":true}}`), nil
			case "signed":
				return reply(`{"retcode":0,"data":{"is_sign":true}}`), nil
			}
			return reply(`{"retcode":0,"data":{"is_sign":false}}`), nil
		case "/event/luna/sign":
			signCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["uid"] != "ready" || r.Header.Get("X-Rpc-Device_id") != "account-device" {
				t.Fatal("wrong role/device")
			}
			return reply(`{"retcode":0,"data":{"success":0}}`), nil
		}
		t.Fatalf("unexpected request %s", r.URL.Path)
		return nil, nil
	})
	cfg := model.Config{Device: model.Device{ID: "account-device"}, Games: model.GamesConfig{Enabled: []string{"genshin"}, Blacklist: map[string][]string{"genshin": {"blocked"}}}}
	result := (GameCheckin{Client: client, Config: cfg}).Run(context.Background())
	if result.Success != 1 || result.Failed != 0 || result.Skipped != 3 || signCalls != 1 || infoCalls != 3 {
		t.Fatalf("unexpected results %+v", result)
	}
}

func TestGameCheckinCaptchaRetriesAreBounded(t *testing.T) {
	for _, retries := range []int{0, 2} {
		t.Run(string(rune('0'+retries)), func(t *testing.T) {
			client := mihoyo.NewClient("")
			signs := 0
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case mihoyo.AccountRolesPath:
					return reply(`{"retcode":0,"data":{"list":[{"game_uid":"1","region":"cn"}]}}`), nil
				case "/event/luna/home":
					return reply(`{"retcode":0,"data":{"awards":[]}}`), nil
				case "/event/luna/info":
					return reply(`{"retcode":0,"data":{"is_sign":false}}`), nil
				case "/event/luna/sign":
					signs++
					return reply(`{"retcode":0,"data":{"success":1,"gt":"gt","challenge":"challenge"}}`), nil
				default:
					return reply(`{"status":0,"msg":"solver failed"}`), nil
				}
			})
			cfg := model.Config{Games: model.GamesConfig{Enabled: []string{"genshin"}}, Captcha: model.CaptchaConfig{MaxRetries: retries}}
			result := (GameCheckin{Client: client, Config: cfg}).Run(context.Background())
			if result.Failed != 1 || result.Success != 0 || signs != 1 {
				t.Fatalf("captcha failure marked success or repeated sign: %+v signs %d", result, signs)
			}
		})
	}
}

func TestCloudCheckinConfirmsWallet(t *testing.T) {
	client := mihoyo.NewClient("")
	calls := 0
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("X-Rpc-Combo_token") != "cloud-secret" {
			t.Fatal("missing cloud token")
		}
		if calls == 1 {
			return reply(`{"retcode":0,"data":{"free_time":{"free_time":100,"send_freetime":0}}}`), nil
		}
		return reply(`{"retcode":0,"data":{"free_time":{"free_time":115,"send_freetime":15}}}`), nil
	})
	cfg := model.Config{CloudGames: model.CloudGamesConfig{Enabled: []string{"genshin", "zzz"}}, BBS: model.BBSConfig{DelaySeconds: []int{0, 0}}}
	result := (CloudCheckin{Client: client, Config: cfg, Account: model.Account{CloudTokens: map[string]string{"genshin": "cloud-secret"}}}).Run(context.Background())
	if calls != 2 || result.Success != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected result %+v, calls %d", result, calls)
	}
}
