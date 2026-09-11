package tasks

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestCustomCaptchaCompletesBBSVerificationBeforeRetry(t *testing.T) {
	client := mihoyo.NewClient("")
	signs, solves, verifies := 0, 0, 0
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case mihoyo.BBSSignPath:
			signs++
			if signs == 1 {
				return reply(`{"retcode":1034}`), nil
			}
			if r.Header.Get("x-rpc-challenge") != "bbs-verified" {
				t.Error("BBS did not use verified challenge")
			}
			return reply(`{"retcode":0,"data":{}}`), nil
		case "/misc/api/createVerification":
			return reply(`{"retcode":0,"data":{"gt":"gt","challenge":"original"}}`), nil
		case "/pass_nine":
			solves++
			if r.Header.Get("Cookie") != "" || r.URL.Query().Get("challenge") != "original" {
				t.Error("custom solver leaked cookie or lost challenge")
			}
			return reply(`{"data":{"result":"success","validate":"custom-validate"}}`), nil
		case "/misc/api/verifyVerification":
			verifies++
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["geetest_challenge"] != "original" || body["geetest_validate"] != "custom-validate" || body["geetest_seccode"] != "custom-validate|jordan" {
				t.Error("BBS verification contract incorrect")
			}
			return reply(`{"retcode":0,"data":{"challenge":"bbs-verified"}}`), nil
		default:
			t.Fatal("unexpected request", r.URL.Path)
			return nil, nil
		}
	})
	cfg := model.Config{Captcha: model.CaptchaConfig{MaxRetries: 2, Channels: []model.CaptchaChannel{{Provider: "custom", Enabled: true, Endpoint: "http://captcha:9645/pass_nine", Timeout: 60}}}}
	_, err := (BBSCheckin{Client: client, Config: cfg, Account: model.Account{Cookie: "private-cookie"}}).request(t.Context(), http.MethodPost, mihoyo.BBSSignPath, nil, map[string]any{"gids": 2}, true)
	if err != nil || signs != 2 || solves != 1 || verifies != 1 {
		t.Fatal("BBS captcha flow failed", err, signs, solves, verifies)
	}
}

func TestCustomCaptchaReachesGameSignHeaders(t *testing.T) {
	client := mihoyo.NewClient("")
	signs, solves := 0, 0
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
			if signs == 1 {
				return reply(`{"retcode":0,"data":{"success":1,"gt":"gt","challenge":"original"}}`), nil
			}
			if r.Header.Get("x-rpc-challenge") != "replacement" || r.Header.Get("x-rpc-validate") != "custom-validate" || r.Header.Get("x-rpc-seccode") != "custom-validate|jordan" {
				t.Error("game did not receive solver headers")
			}
			return reply(`{"retcode":0,"data":{"success":0}}`), nil
		case "/pass_uni":
			solves++
			if r.Header.Get("Cookie") != "" {
				t.Error("game cookie sent to solver")
			}
			return reply(`{"data":{"result":"success","validate":"custom-validate","challenge":"replacement"}}`), nil
		default:
			t.Fatal("unexpected request", r.URL.Path)
			return nil, nil
		}
	})
	cfg := model.Config{Games: model.GamesConfig{Enabled: []string{"genshin"}}, Captcha: model.CaptchaConfig{MaxRetries: 2, Channels: []model.CaptchaChannel{{Provider: "custom", Enabled: true, Endpoint: "http://captcha:9645/pass_uni", Timeout: 60}}}}
	result := (GameCheckin{Client: client, Config: cfg, Account: model.Account{Cookie: "private-cookie"}}).Run(t.Context())
	if result.Failed != 0 || result.Success != 1 || signs != 2 || solves != 1 {
		t.Fatal("game captcha flow failed", result, signs, solves)
	}
}
