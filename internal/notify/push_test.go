package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func testChannel(provider string) model.PushChannel {
	return model.PushChannel{ID: "push_test", Name: "测试渠道", Provider: provider, Enabled: true,
		Token: "123:token-test", Webhook: "https://hooks.test/send?key=private-hook", Secret: "secret-test", ChatID: "-1001",
		APIURL: "https://telegram.test/api", AppID: "app_1", ClientSecret: "client-secret-test", OpenID: "uid_1", Topic: "topic-test",
		PushURL: "http://onebot.test/api", AccessToken: "access-test", SendID: "10001", MsgType: "group",
		SMTPHost: "smtp.test", SMTPPort: 465, SMTPSSL: true, SMTPUser: "from@example.test", SMTPPassword: "smtp-secret-test", MailTo: "to@example.test"}
}
func sendTest(s Sender, channel model.PushChannel) Result {
	return s.Send(context.Background(), model.PushConfig{Enabled: true, Channels: []model.PushChannel{channel}}, "测试 <标题>", "一行\n<script>正文</script>", false)[0]
}

func TestHTTPProviderContracts(t *testing.T) {
	cases := []struct{ provider, host, path, reply string }{
		{"pushplus", "www.pushplus.plus", "/send", `{"code":200}`},
		{"telegram", "telegram.test", "/api/bot123:token-test/sendMessage", `{"ok":true}`},
		{"wxpusher", "wxpusher.zjiecode.com", "/api/send/message", `{"code":1000,"success":true,"data":[{"code":1000}]}`},
		{"dingrobot", "hooks.test", "/send", `{"errcode":0}`},
		{"feishubot", "hooks.test", "/send", `{"code":0}`},
		{"qqbot", "api.sgroup.qq.com", "/v2/users/uid_1/messages", `{"id":"message-id"}`},
		{"qq", "onebot.test", "/api/send_msg", `{"retcode":0,"status":"ok","data":{"message_id":12}}`},
		{"wechat_claw", "hooks.test", "/send", `{"success":true}`},
		{"webhook", "hooks.test", "/send", "accepted"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			calls := 0
			channel := testChannel(tc.provider)
			s := Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
					t.Error("invalid HTTP method or content type")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if tc.provider == "qqbot" && calls == 1 {
					if r.URL.Host != "bots.qq.com" || r.URL.Path != "/app/getAppAccessToken" || body["appId"] != channel.AppID || body["clientSecret"] != channel.ClientSecret {
						t.Error("invalid QQ authorization request")
					}
					return response(`{"access_token":"fresh-token","expires_in":"7200"}`), nil
				}
				if r.URL.Host != tc.host || r.URL.Path != tc.path {
					t.Errorf("unexpected destination %s%s", r.URL.Host, r.URL.Path)
				}
				switch tc.provider {
				case "pushplus":
					if body["topic"] != channel.Topic || body["token"] != channel.Token || body["template"] != "txt" {
						t.Error("invalid PushPlus body")
					}
				case "telegram":
					if body["chat_id"] != channel.ChatID || body["parse_mode"] != nil {
						t.Error("Telegram text should be plain text")
					}
				case "wxpusher":
					content, _ := body["content"].(string)
					if strings.Contains(content, "<script>") || !strings.Contains(content, "&lt;script&gt;") || body["appToken"] != channel.Token {
						t.Error("WxPusher HTML was not escaped")
					}
				case "dingrobot":
					q := r.URL.Query()
					sign := hmac.New(sha256.New, []byte(channel.Secret))
					_, _ = sign.Write([]byte(q.Get("timestamp") + "\n" + channel.Secret))
					if q.Get("key") != "private-hook" || q.Get("timestamp") == "" || q.Get("sign") != base64.StdEncoding.EncodeToString(sign.Sum(nil)) || body["msgtype"] != "text" {
						t.Error("DingTalk signature or payload is invalid")
					}
				case "feishubot":
					ts, _ := body["timestamp"].(string)
					sign := hmac.New(sha256.New, []byte(ts+"\n"+channel.Secret))
					content, _ := body["content"].(map[string]any)
					if body["msg_type"] != "text" || content["text"] == nil || body["sign"] != base64.StdEncoding.EncodeToString(sign.Sum(nil)) {
						t.Error("Feishu signature or payload is invalid")
					}
				case "qqbot":
					if r.Header.Get("Authorization") != "QQBot fresh-token" || body["msg_type"] != float64(0) {
						t.Error("invalid QQ message")
					}
				case "qq":
					if r.Header.Get("Authorization") != "Bearer access-test" || body["group_id"] != channel.SendID || body["user_id"] != nil || body["auto_escape"] != true {
						t.Error("invalid OneBot message")
					}
				case "wechat_claw", "webhook":
					if r.Header.Get("Authorization") != "Bearer "+channel.Token || body["title"] == nil || body["markdown"] == nil {
						t.Error("invalid webhook body")
					}
				}
				return response(tc.reply), nil
			})}}
			got := sendTest(s, channel)
			if !got.OK || got.Error != "" || got.ChannelID != channel.ID || got.Provider != tc.provider {
				t.Fatalf("delivery failed: %+v", got)
			}
			want := 1
			if tc.provider == "qqbot" {
				want = 2
			}
			if calls != want {
				t.Errorf("requests=%d, want %d", calls, want)
			}
		})
	}
}

func TestProviderBusinessFailuresAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct{ provider, reply string }{
		{"pushplus", `{"code":500,"msg":"token=private-secret"}`},
		{"telegram", `{"ok":false,"error_code":401,"description":"private-secret"}`},
		{"telegram", `{"ok":null}`},
		{"wxpusher", `{"code":1000,"success":false}`},
		{"wxpusher", `{"code":1000,"data":[{"code":1008}]}`},
		{"dingrobot", `{"errcode":310000}`},
		{"dingrobot", `{"errcode":null}`},
		{"dingrobot", `{"errcode":"0"}`},
		{"feishubot", `{"code":19021,"StatusCode":0}`},
		{"feishubot", `{}`},
		{"qqbot", `{"code":400,"message":"private-secret"}`},
		{"qq", `{"retcode":0,"status":"failed"}`},
		{"qq", `{"retcode":null,"status":"ok"}`},
		{"webhook", `{"ok":false}`},
		{"webhook", `{"code":500}`},
		{"wechat_claw", `{"success":false}`},
		{"pushplus", `<html>private-secret</html>`},
		{"pushplus", `null`},
	} {
		t.Run(tc.provider+tc.reply, func(t *testing.T) {
			err := validateResponse(tc.provider, []byte(tc.reply))
			if err == nil {
				t.Fatal("business error accepted as success")
			}
			if strings.Contains(err.Error(), "private-secret") {
				t.Fatal("upstream secrets leaked")
			}
		})
	}
}

func TestKnownProviderRejectionIsFailedNotUnknown(t *testing.T) {
	for _, tc := range []struct{ provider, reply string }{
		{"pushplus", `{"code":401}`},
		{"telegram", `{"ok":false,"error_code":403}`},
		{"wxpusher", `{"code":1000,"data":[{"code":1008}]}`},
		{"dingrobot", `{"errcode":310000}`},
		{"feishubot", `{"code":19021}`},
		{"qq", `{"retcode":1404,"status":"failed"}`},
		{"webhook", `{"ok":false}`},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return response(tc.reply), nil
			})}}
			if result := sendTest(sender, testChannel(tc.provider)); result.OK || result.Uncertain || result.Error == "" {
				t.Fatal("known rejection was reported as unknown or successful", result)
			}
		})
	}
}

func TestSenderGatesRedirectsAndSanitizesErrors(t *testing.T) {
	c := testChannel("webhook")
	calls := 0
	s := Sender{HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("token=private-secret")
	})}}
	for _, cfg := range []model.PushConfig{{Channels: []model.PushChannel{c}}, {Enabled: true, ErrorOnly: true, Channels: []model.PushChannel{c}}, {Enabled: true, Channels: []model.PushChannel{{Provider: "webhook", Enabled: false}}}} {
		if got := s.Send(context.Background(), cfg, "title", "message", true); len(got) != 0 {
			t.Fatal("disabled notifications were sent")
		}
	}
	if calls != 0 {
		t.Fatal("a gate reached the network")
	}
	got := sendTest(s, c)
	raw, _ := json.Marshal(got)
	if got.OK || got.Error == "" || strings.Contains(string(raw), "private-secret") || strings.Contains(string(raw), "hooks.test") || calls != 1 {
		t.Fatalf("unsafe error or retry: %s", raw)
	}
	calls = 0
	s.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		r := response("")
		r.StatusCode = 307
		r.Header.Set("Location", "https://elsewhere.test/secret")
		return r, nil
	})
	if got := sendTest(s, c); got.OK || !strings.Contains(got.Error, "307") || calls != 1 {
		t.Fatal("secret redirect was followed or reported as success", got, calls)
	}
	s.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) { return response(strings.Repeat("x", 128*1024+1)), nil })
	if sendTest(s, c).OK {
		t.Fatal("oversized body accepted")
	}
	s.Timeout = 10 * time.Millisecond
	s.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if got := sendTest(s, c); got.OK || !got.Uncertain || !strings.Contains(got.Error, "超时") {
		t.Fatal("timeout not handled", got)
	}
}

func TestValidateChannels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*model.PushChannel)
	}{
		{"unknown", func(c *model.PushChannel) { c.Provider = "unknown" }},
		{"credentials in URL", func(c *model.PushChannel) { c.Webhook = "https://user:secret@localhost/send" }},
		{"scheme", func(c *model.PushChannel) { c.Webhook = "file:///tmp/test" }},
		{"fragment", func(c *model.PushChannel) { c.Webhook = "https://test/a#secret" }},
		{"port", func(c *model.PushChannel) { c.Webhook = "https://test:70000/a" }},
		{"header", func(c *model.PushChannel) { c.Token += "\r\nHeader: value" }},
		{"query in base", func(c *model.PushChannel) { c.APIURL = "https://test/api?token=x" }},
		{"telegram token", func(c *model.PushChannel) { c.Provider = "telegram"; c.Token = "../../../secret" }},
		{"qq target", func(c *model.PushChannel) { c.Provider = "qq"; c.SendID = "not-a-number" }},
		{"qqbot target", func(c *model.PushChannel) { c.Provider = "qqbot"; c.OpenID = "../../x" }},
		{"SMTP host", func(c *model.PushChannel) { c.Provider = "email"; c.SMTPHost = "https://smtp.test" }},
		{"SMTP credentials", func(c *model.PushChannel) { c.Provider = "email"; c.SMTPPassword = "" }},
		{"SMTP recipient", func(c *model.PushChannel) { c.Provider = "email"; c.MailTo = "invalid-address" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testChannel("webhook")
			tc.mutate(&c)
			if ValidateChannel(c, true) == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	if err := ValidateChannel(model.PushChannel{Provider: "telegram", Name: "draft"}, false); err != nil {
		t.Fatal("disabled draft rejected", err)
	}
	if err := ValidateChannel(model.PushChannel{Provider: "telegram", Name: "draft"}, true); err == nil {
		t.Fatal("incomplete channel enabled")
	}
	if err := ValidateEndpoint("http://127.0.0.1:3000/send", false); err != nil {
		t.Fatal("admin self-hosted endpoint rejected", err)
	}
}

func TestChannelFailureDoesNotStopOtherChannels(t *testing.T) {
	calls := 0
	sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(`{"ok":false}`), nil
		}
		return response(`{"ok":true}`), nil
	})}}
	results := sender.Send(context.Background(), model.PushConfig{Enabled: true, Channels: []model.PushChannel{testChannel("webhook"), testChannel("webhook")}}, "title", "text", false)
	if calls != 2 || len(results) != 2 || results[0].OK || !results[1].OK {
		t.Fatal("first failure prevented another channel", results)
	}
}
