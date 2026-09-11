package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type bindingSink struct {
	mu      sync.Mutex
	calls   int
	user    string
	channel model.PushChannel
}

func (s *bindingSink) BindPushChannel(user, id string, _ int, c model.PushChannel) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.user = user
	s.channel = c
	if id == "" {
		id = "bound-channel"
	}
	return id, nil
}
func (s *bindingSink) snapshot() (int, string, model.PushChannel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.user, s.channel
}
func waitBinding(t *testing.T, m *Bindings, user, status string) BindingState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := m.State(user)
		if state.Status == status {
			return state
		}
		if state.Status == "error" {
			t.Fatalf("binding failed: %s", state.Message)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("binding never reached %s: %+v", status, m.State(user))
	return BindingState{}
}
func encryptTestSecret(key []byte, secret string) string {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, 12)
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, []byte(secret), nil)...))
}

func TestQQOfficialScanDecryptsCredentialsAndBindsScanningUser(t *testing.T) {
	sink := &bindingSink{}
	var key []byte
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "q.qq.com" || r.Method != "POST" {
			t.Error("wrong QQ binding endpoint")
		}
		switch r.URL.Path {
		case "/lite/create_bind_task":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			key, _ = base64.StdEncoding.DecodeString(body["key"])
			if len(key) != 32 {
				t.Error("invalid binding key")
			}
			return response(`{"retcode":0,"data":{"task_id":"task-123"}}`), nil
		case "/lite/poll_bind_result":
			return response(fmt.Sprintf(`{"retcode":0,"data":{"status":2,"bot_appid":123456,"bot_encrypt_secret":%q,"user_openid":"scanner_openid"}}`, encryptTestSecret(key, "actual-client-secret"))), nil
		}
		t.Error("unexpected QR request")
		return response(`{}`), nil
	})}
	m := NewBindings(sink, Sender{HTTP: client})
	m.pollInterval = time.Millisecond
	defer m.Stop()
	if _, err := m.Start("owner", "qqbot", "", 1); err != nil {
		t.Fatal(err)
	}
	state := waitBinding(t, m, "owner", "confirmed")
	calls, user, channel := sink.snapshot()
	if calls != 1 || user != "owner" || channel.AppID != "123456" || channel.ClientSecret != "actual-client-secret" || channel.OpenID != "scanner_openid" {
		t.Fatal("QQ credentials or receiver were not bound correctly")
	}
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), "actual-client-secret") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(key)) {
		t.Fatal("binding state leaked secrets")
	}
	if m.State("other-user").SessionID != "" {
		t.Fatal("QR state crossed users")
	}
}

func TestWeixinOfficialPairingCodeAndScannerBinding(t *testing.T) {
	sink := &bindingSink{}
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("iLink-App-Id") != "bot" {
			t.Error("missing official app header")
		}
		if r.URL.Path == "/ilink/bot/get_bot_qrcode" {
			if r.Method != "POST" || r.URL.Query().Get("bot_type") != "3" {
				t.Error("incorrect QR request")
			}
			return response(`{"qrcode":"weixin-ticket","qrcode_img_content":"https://ilinkai.weixin.qq.com/qr/test"}`), nil
		}
		if r.URL.Path != "/ilink/bot/get_qrcode_status" || r.Method != "GET" {
			t.Error("incorrect polling path")
		}
		if r.URL.Query().Get("verify_code") == "" {
			return response(`{"status":"need_verifycode"}`), nil
		}
		if r.URL.Query().Get("verify_code") != "123456" {
			t.Error("pairing code not forwarded")
		}
		return response(`{"status":"confirmed","bot_token":"weixin-private-token","ilink_bot_id":"bot-test","ilink_user_id":"scanner-user","baseurl":"https://ilinkai.weixin.qq.com"}`), nil
	})}
	m := NewBindings(sink, Sender{HTTP: client})
	m.pollInterval = time.Millisecond
	defer m.Stop()
	_, err := m.Start("owner", "wechat_claw", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	state := waitBinding(t, m, "owner", "need_verifycode")
	if !strings.HasPrefix(state.QRImage, "data:image/png;base64,") {
		t.Fatal("QR image was not rendered locally")
	}
	if m.Verify("other-user", state.SessionID, "123456") == nil {
		t.Fatal("foreign user submitted pairing code")
	}
	if m.Verify("owner", state.SessionID, "not-numeric") == nil {
		t.Fatal("invalid pairing code accepted")
	}
	if err = m.Verify("owner", state.SessionID, "123456"); err != nil {
		t.Fatal(err)
	}
	waitBinding(t, m, "owner", "confirmed")
	calls, user, c := sink.snapshot()
	if calls != 1 || user != "owner" || c.OpenID != "scanner-user" || c.Mode != "ilink" || c.BindingState != "waiting_message" {
		t.Fatal("Weixin binding did not capture scanner identity")
	}
}

func TestCancelledBindingCannotPersistLateCredentials(t *testing.T) {
	sink := &bindingSink{}
	entered := make(chan struct{})
	release := make(chan struct{})
	var key []byte
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "create_bind_task") {
			var data map[string]string
			_ = json.NewDecoder(r.Body).Decode(&data)
			key, _ = base64.StdEncoding.DecodeString(data["key"])
			return response(`{"retcode":0,"data":{"task_id":"old-task"}}`), nil
		}
		close(entered)
		<-release
		return response(fmt.Sprintf(`{"retcode":0,"data":{"status":2,"bot_appid":"123","bot_encrypt_secret":%q,"user_openid":"old-owner"}}`, encryptTestSecret(key, "old-secret"))), nil
	})}
	m := NewBindings(sink, Sender{HTTP: client})
	m.pollInterval = time.Millisecond
	state, err := m.Start("owner", "qqbot", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("QR poll did not start")
	}
	m.Cancel("owner", state.SessionID)
	close(release)
	m.Stop()
	if calls, _, _ := sink.snapshot(); calls != 0 {
		t.Fatal("cancelled binding persisted credentials")
	}
}

func TestBindingRestoresSameSessionWithoutExtendingExpiryOrCrossingUsers(t *testing.T) {
	var creates atomic.Int32
	m := NewBindings(&bindingSink{}, Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "create_bind_task") {
			creates.Add(1)
			return response(`{"retcode":0,"data":{"task_id":"fixture-task"}}`), nil
		}
		return response(`{"retcode":0,"data":{"status":0}}`), nil
	})}})
	m.pollInterval = time.Hour
	defer m.Stop()
	first, err := m.Start("owner", "qqbot", "channel", 1)
	if err != nil {
		t.Fatal(err)
	}
	waitBinding(t, m, "owner", "waiting")
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			got, err := m.Start("owner", "qqbot", "channel", 1)
			if err != nil || got.SessionID != first.SessionID || !got.ExpiresAt.Equal(first.ExpiresAt) || got.Revision != 1 {
				t.Error("restoring a session restarted it or extended its expiry")
			}
		})
	}
	wg.Wait()
	if creates.Load() != 1 {
		t.Fatal("restoring a session repeated the upstream QR request")
	}
	m.Cancel("other-user", first.SessionID)
	if !m.State("owner").Running || m.State("other-user").SessionID != "" {
		t.Fatal("another user could read or cancel the session")
	}
	updated, err := m.Start("owner", "qqbot", "channel", 2)
	if err != nil || updated.SessionID == first.SessionID {
		t.Fatal("changed configuration restored a stale binding")
	}
	m.Cancel("owner", first.SessionID)
	if !m.State("owner").Running {
		t.Fatal("an old tab cancelled the replacement session")
	}
	m.Cancel("owner", updated.SessionID)
	if got := m.State("owner"); got.Running || got.QRImage != "" || got.QRURL != "" {
		t.Fatal("explicit cancellation left an active QR")
	}
	restarted, err := m.Start("owner", "qqbot", "channel", 2)
	if err != nil || restarted.SessionID == updated.SessionID {
		t.Fatal("cancelled session was restored")
	}
	m.mu.Lock()
	m.sessions["owner"].state.ExpiresAt = time.Now().Add(-time.Second)
	m.mu.Unlock()
	renewed, err := m.Start("owner", "qqbot", "channel", 2)
	if err != nil || renewed.SessionID == restarted.SessionID {
		t.Fatal("expired session was restored")
	}
}

func TestQQSecretAuthenticationAndWeixinRedirectValidation(t *testing.T) {
	key := make([]byte, 32)
	encoded := encryptTestSecret(key, "secret")
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	raw[len(raw)-1] ^= 1
	if _, err := decryptQQSecret(base64.StdEncoding.EncodeToString(raw), key); err == nil {
		t.Fatal("tampered QQ secret accepted")
	}
	for _, link := range []string{"http://ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com.evil.test", "https://user@ilinkai.weixin.qq.com", "https://127.0.0.1", "https://ilinkai.weixin.qq.com/path", "https://ilinkai.weixin.qq.com?token=test"} {
		if WeixinEndpoint(link) {
			t.Fatal("untrusted Weixin endpoint", link)
		}
	}
	if !WeixinEndpoint(weixinBase) {
		t.Fatal("official endpoint rejected")
	}
	sink := &bindingSink{}
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "ilinkai.weixin.qq.com" {
			t.Error("followed hostile redirect")
		}
		if strings.HasSuffix(r.URL.Path, "get_bot_qrcode") {
			return response(`{"qrcode":"ticket","qrcode_img_content":"https://ilinkai.weixin.qq.com/qr/test"}`), nil
		}
		return response(`{"status":"scaned_but_redirect","redirect_host":"127.0.0.1"}`), nil
	})}
	m := NewBindings(sink, Sender{HTTP: client})
	m.pollInterval = time.Millisecond
	defer m.Stop()
	_, _ = m.Start("owner", "wechat_claw", "", 1)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && m.State("owner").Status != "error" {
		time.Sleep(time.Millisecond)
	}
	if m.State("owner").Status != "error" {
		t.Fatal("hostile redirect was not rejected")
	}
	if calls, _, _ := sink.snapshot(); calls != 0 {
		t.Fatal("hostile redirect persisted binding")
	}
}

func TestWeixinMessageContractAndBusinessErrors(t *testing.T) {
	c := model.PushChannel{ID: "wx", Name: "微信", Provider: "wechat_claw", Mode: "ilink", Enabled: true, Token: "private-token", OpenID: "owner", BotID: "bot", APIURL: weixinBase, ContextToken: "context-private"}
	for _, reply := range []string{`{"ret":0}`, `{"ret":-14,"errmsg":"private-token"}`, `{}`} {
		sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/ilink/bot/sendmessage" || r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("AuthorizationType") != "ilink_bot_token" {
				t.Error("Weixin wire contract mismatch")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			msg, _ := body["msg"].(map[string]any)
			if msg["to_user_id"] != "owner" || msg["context_token"] != "context-private" || msg["message_type"] != float64(2) || msg["message_state"] != float64(2) {
				t.Error("incorrect Weixin text envelope")
			}
			return response(reply), nil
		})}}
		got := sendTest(sender, c)
		if got.OK != (reply == `{"ret":0}`) || strings.Contains(got.Error, "private-token") {
			t.Fatal("Weixin business status or redaction incorrect", got)
		}
	}
	c.ContextToken = ""
	if got := sendTest(Sender{}, c); got.OK || !strings.Contains(got.Error, "先在微信") {
		t.Fatal("empty context reported ready")
	}
}

type sessionSink struct{ captured chan string }

func (s sessionSink) PrivatePushConfigs() map[string]model.PushConfig { return nil }
func (s sessionSink) UpdateWeixinSession(_ string, _ model.PushChannel, _ string, token, _, _ string) bool {
	s.captured <- token
	return false
}
func TestWeixinReceiverOnlyRetainsBoundUsersContext(t *testing.T) {
	sink := sessionSink{captured: make(chan string, 1)}
	sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(`{"ret":0,"get_updates_buf":"next","msgs":[{"from_user_id":"stranger","message_type":1,"context_token":"foreign"},{"from_user_id":"owner","message_type":1,"context_token":"correct"}]}`), nil
	})}}
	m := NewWeixinMonitor(sink, sender)
	m.poll(context.Background(), "user", model.PushChannel{Token: "token", OpenID: "owner", APIURL: weixinBase})
	if token := <-sink.captured; token != "correct" {
		t.Fatal("foreign conversation used as notification target")
	}
}
